package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// --- fake tool -----------------------------------------------------------

type fakeTool struct {
	name    string
	content string
	isError bool
	// panics if true
	panics bool
}

func (f *fakeTool) Name() string                 { return f.name }
func (f *fakeTool) Description() string          { return "fake tool" }
func (f *fakeTool) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (f *fakeTool) Execute(_ context.Context, _ json.RawMessage, _ tools.ProgressSink) (tools.Result, error) {
	if f.panics {
		panic("intentional panic from fakeTool")
	}
	return tools.Result{Content: f.content, IsError: f.isError}, nil
}

// compileEmptyGuard returns a Guard in yolo mode (allows everything without prompting).
func compileYoloGuard(t *testing.T) *guard.Guard {
	t.Helper()
	wl, err := guard.Compile(guard.WhitelistFile{}, guard.WhitelistFile{}, "")
	if err != nil {
		t.Fatalf("guard.Compile: %v", err)
	}
	g := guard.NewGuard(wl, nil) // nil frontend is fine in yolo mode
	g.SetYolo(true)
	return g
}

// compileDenyGuard returns a Guard backed by a frontend that always denies.
func compileDenyGuard(t *testing.T, fe *fakeRecorder) *guard.Guard {
	t.Helper()
	wl, err := guard.Compile(guard.WhitelistFile{}, guard.WhitelistFile{}, "")
	if err != nil {
		t.Fatalf("guard.Compile: %v", err)
	}
	// Wire the fakeRecorder as the guard.Frontend.
	// fakeRecorder.approval defaults to zero value; Outcome "" maps to unknown_outcome -> deny.
	gfe := &guardFE{fe: fe}
	return guard.NewGuard(wl, gfe)
}

// guardFE bridges fakeRecorder to guard.Frontend.
type guardFE struct{ fe *fakeRecorder }

func (g *guardFE) RequestApproval(ctx context.Context, req guard.ApprovalRequest) (guard.ApprovalDecision, error) {
	return g.fe.RequestApproval(ctx, req)
}

// newTestAgentWithGuard creates an Agent with a given guard.
func newTestAgentWithGuard(t *testing.T, client common.Client, fe *fakeRecorder, g *guard.Guard, reg *tools.Registry) (*Agent, *session.Session) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	w, err := session.OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	sess := session.NewSession("sess-run", dir, "model", "anthropic")
	a := &Agent{
		Tools:    reg,
		Guard:    g,
		Frontend: fe,
		State:    NewSessionState(sess, w, client),
		System:   "system",
	}
	return a, sess
}

// TestRun_ToolDispatch verifies the full two-turn loop:
//   - first turn: text + tool_use
//   - second turn: text + end_turn
//   - tool executed, result appended as RoleTool message
//   - Run returns nil
func TestRun_ToolDispatch(t *testing.T) {
	turn1 := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: "thinking"},
		{Kind: common.EventToolCallDone, ToolCallID: "tc1", ToolName: "fake", InputJSON: `{}`},
		{Kind: common.EventTurnDone, StopReason: "tool_use"},
	}
	turn2 := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: "done"},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{turn1, turn2}}

	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "fake", content: "tool output"})

	fe := &fakeRecorder{}
	g := compileYoloGuard(t)
	a, sess := newTestAgentWithGuard(t, client, fe, g, reg)

	if err := a.Run(context.Background(), sess); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// History: assistant(turn1) + tool_result + assistant(turn2)
	if len(sess.History) != 3 {
		t.Fatalf("sess.History length: got %d, want 3\n%+v", len(sess.History), sess.History)
	}

	// The tool-result message must be RoleTool.
	toolMsg := sess.History[1]
	if toolMsg.Role != common.RoleTool {
		t.Errorf("toolMsg.Role: got %v, want RoleTool", toolMsg.Role)
	}
	if len(toolMsg.Content) != 1 {
		t.Fatalf("toolMsg.Content blocks: got %d, want 1", len(toolMsg.Content))
	}
	if toolMsg.Content[0].Kind != common.BlockToolResult {
		t.Errorf("block kind: got %v, want BlockToolResult", toolMsg.Content[0].Kind)
	}
	if toolMsg.Content[0].ToolUseID != "tc1" {
		t.Errorf("ToolUseID: got %q, want tc1", toolMsg.Content[0].ToolUseID)
	}
	if toolMsg.Content[0].ResultText != "tool output" {
		t.Errorf("ResultText: got %q, want %q", toolMsg.Content[0].ResultText, "tool output")
	}

	// Frontend should have seen EventToolResult.
	var sawToolResult bool
	for _, ev := range fe.events {
		if ev.Kind == EventToolResult {
			sawToolResult = true
			if ev.Result == nil {
				t.Error("EventToolResult.Result is nil")
			} else if ev.Result.Content != "tool output" {
				t.Errorf("EventToolResult.Result.Content: got %q", ev.Result.Content)
			}
		}
	}
	if !sawToolResult {
		t.Errorf("no EventToolResult in frontend events")
	}
}

// TestRun_DeniedTool verifies that when the guard denies, the tool is NOT
// executed, an IsError tool result is appended, and Run returns ErrToolDenied.
func TestRun_DeniedTool(t *testing.T) {
	turn1 := []common.StreamEvent{
		{Kind: common.EventToolCallDone, ToolCallID: "tc-deny", ToolName: "fake", InputJSON: `{}`},
		{Kind: common.EventTurnDone, StopReason: "tool_use"},
	}
	// No turn2 needed: Run should return ErrToolDenied before calling Stream again.
	client := &fakeClient{sequences: [][]common.StreamEvent{turn1}}

	executed := false
	tracked := &trackingTool{
		fakeTool: &fakeTool{name: "fake", content: "should-not-run"},
		executed: &executed,
	}
	reg := tools.NewRegistry()
	reg.Register(tracked)

	fe := &fakeRecorder{
		approval: guard.ApprovalDecision{Outcome: ""},
	}
	g := compileDenyGuard(t, fe)
	a, sess := newTestAgentWithGuard(t, client, fe, g, reg)

	err := a.Run(context.Background(), sess)
	if !errors.Is(err, ErrToolDenied) {
		t.Fatalf("Run: got %v, want ErrToolDenied", err)
	}

	if executed {
		t.Error("tool was executed despite being denied")
	}

	// The tool-result message must exist and be IsError.
	var foundToolMsg bool
	for _, msg := range sess.History {
		if msg.Role == common.RoleTool {
			foundToolMsg = true
			for _, b := range msg.Content {
				if !b.IsError {
					t.Errorf("denied tool result block should have IsError=true")
				}
			}
		}
	}
	if !foundToolMsg {
		t.Errorf("no RoleTool message in history after denial")
	}

	// Verify Run only called Stream once (did not proceed to a second turn).
	if client.callCount != 1 {
		t.Errorf("Stream call count: got %d, want 1", client.callCount)
	}

	// Frontend should have seen EventToolDenied.
	var sawDenied bool
	for _, ev := range fe.events {
		if ev.Kind == EventToolDenied {
			sawDenied = true
		}
	}
	if !sawDenied {
		t.Errorf("no EventToolDenied in frontend events")
	}
}

// trackingTool wraps fakeTool and records whether Execute was called.
type trackingTool struct {
	*fakeTool
	executed *bool
}

func (tr *trackingTool) Execute(ctx context.Context, in json.RawMessage, sink tools.ProgressSink) (tools.Result, error) {
	*tr.executed = true
	return tr.fakeTool.Execute(ctx, in, sink)
}

// TestRun_DenySteer verifies that when the guard returns DenySteer:
//   - the tool is NOT executed
//   - the synthesised tool_result has the steer message as content and IsError true
//   - Run drives a second Turn (the fake client ends with end_turn on turn 2)
func TestRun_DenySteer(t *testing.T) {
	turn1 := []common.StreamEvent{
		{Kind: common.EventToolCallDone, ToolCallID: "tc-steer", ToolName: "fake", InputJSON: `{}`},
		{Kind: common.EventTurnDone, StopReason: "tool_use"},
	}
	turn2 := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: "ok"},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{turn1, turn2}}

	executed := false
	tracked := &trackingTool{
		fakeTool: &fakeTool{name: "fake", content: "should-not-run"},
		executed: &executed,
	}
	reg := tools.NewRegistry()
	reg.Register(tracked)

	const steerMsg = "use X instead"
	fe := &fakeRecorder{
		approval: guard.ApprovalDecision{
			Outcome:      guard.DenySteer,
			SteerMessage: steerMsg,
		},
	}
	// Real (non-yolo) guard backed by the fakeRecorder as the guard frontend.
	gfe := &guardFE{fe: fe}
	wl, err := guard.Compile(guard.WhitelistFile{}, guard.WhitelistFile{}, "")
	if err != nil {
		t.Fatalf("guard.Compile: %v", err)
	}
	g := guard.NewGuard(wl, gfe)

	a, sess := newTestAgentWithGuard(t, client, fe, g, reg)

	if err := a.Run(context.Background(), sess); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if executed {
		t.Error("tool was executed despite DenySteer")
	}

	// Find the RoleTool message and verify steer content + IsError.
	var foundSteer bool
	for _, msg := range sess.History {
		if msg.Role != common.RoleTool {
			continue
		}
		for _, b := range msg.Content {
			if b.ToolUseID == "tc-steer" {
				foundSteer = true
				if !b.IsError {
					t.Errorf("DenySteer result: IsError want true, got false")
				}
				if b.ResultText != steerMsg {
					t.Errorf("DenySteer result: content = %q, want %q", b.ResultText, steerMsg)
				}
			}
		}
	}
	if !foundSteer {
		t.Errorf("no tool result block for tc-steer found in history")
	}

	// Verify Run called Stream twice (second turn end_turn).
	if client.callCount != 2 {
		t.Errorf("Stream call count: got %d, want 2", client.callCount)
	}
}

// TestRun_MixedBatch verifies that when a batch has one approved and one
// plain-denied tool call, Run returns ErrToolDenied but the approved tool
// still executes and both results appear in history.
func TestRun_MixedBatch(t *testing.T) {
	turn1 := []common.StreamEvent{
		{Kind: common.EventToolCallDone, ToolCallID: "tc-ok", ToolName: "allowed", InputJSON: `{}`},
		{Kind: common.EventToolCallDone, ToolCallID: "tc-no", ToolName: "denied", InputJSON: `{}`},
		{Kind: common.EventTurnDone, StopReason: "tool_use"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{turn1}}

	executedAllowed := false
	executedDenied := false

	reg := tools.NewRegistry()
	reg.Register(&trackingTool{
		fakeTool: &fakeTool{name: "allowed", content: "ok"},
		executed: &executedAllowed,
	})
	reg.Register(&trackingTool{
		fakeTool: &fakeTool{name: "denied", content: "should-not-run"},
		executed: &executedDenied,
	})

	callCount := 0
	fe := &fakeRecorder{
		approvalFunc: func(_ context.Context, req guard.ApprovalRequest) (guard.ApprovalDecision, error) {
			callCount++
			if req.Tool == "allowed" {
				return guard.ApprovalDecision{Outcome: guard.AllowOnce}, nil
			}
			return guard.ApprovalDecision{Outcome: guard.Deny}, nil
		},
	}

	wl, err := guard.Compile(guard.WhitelistFile{}, guard.WhitelistFile{}, "")
	if err != nil {
		t.Fatalf("guard.Compile: %v", err)
	}
	gfe := &guardFE{fe: fe}
	g := guard.NewGuard(wl, gfe)

	a, sess := newTestAgentWithGuard(t, client, fe, g, reg)

	runErr := a.Run(context.Background(), sess)
	if !errors.Is(runErr, ErrToolDenied) {
		t.Fatalf("Run: got %v, want ErrToolDenied", runErr)
	}

	if !executedAllowed {
		t.Error("allowed tool was not executed")
	}
	if executedDenied {
		t.Error("denied tool was executed")
	}

	// History should have: assistant (turn1) + tool results.
	// The RoleTool message should have 2 blocks.
	var toolMsg *common.Message
	for i := range sess.History {
		if sess.History[i].Role == common.RoleTool {
			toolMsg = &sess.History[i]
			break
		}
	}
	if toolMsg == nil {
		t.Fatal("no RoleTool message in history")
	}
	if len(toolMsg.Content) != 2 {
		t.Fatalf("RoleTool blocks: got %d, want 2", len(toolMsg.Content))
	}

	// First block (allowed) should not be error.
	if toolMsg.Content[0].IsError {
		t.Errorf("allowed tool result should not be IsError")
	}
	// Second block (denied) should be error.
	if !toolMsg.Content[1].IsError {
		t.Errorf("denied tool result should be IsError")
	}

	// Run should NOT have called Stream a second time.
	if client.callCount != 1 {
		t.Errorf("Stream call count: got %d, want 1", client.callCount)
	}
}

// --- spy tool that captures context -----------------------------------------

type spyContextTool struct {
	onExecute func(ctx context.Context)
}

func (s *spyContextTool) Name() string                 { return "spy" }
func (s *spyContextTool) Description() string          { return "spy tool" }
func (s *spyContextTool) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (s *spyContextTool) Execute(ctx context.Context, in json.RawMessage, sink tools.ProgressSink) (tools.Result, error) {
	if s.onExecute != nil {
		s.onExecute(ctx)
	}
	return tools.Result{Content: "ok"}, nil
}

// TestDispatchTool_SudoPasswordInContext verifies that when the guard returns
// a Decision with SudoPassword set, the agent injects it into the tool's
// context so that tools.SudoPasswordFrom can retrieve it.
func TestDispatchTool_SudoPasswordInContext(t *testing.T) {
	// Create a spy tool that captures the context it receives.
	var capturedCtx context.Context
	spy := &spyContextTool{
		onExecute: func(ctx context.Context) {
			capturedCtx = ctx
		},
	}

	reg := tools.NewRegistry()
	reg.Register(spy)

	// Create a guard with a frontend that returns AllowOnce + SudoPassword.
	fe := &fakeRecorder{
		approval: guard.ApprovalDecision{
			Outcome:      guard.AllowOnce,
			SudoPassword: "secret123",
		},
	}
	gfe := &guardFE{fe: fe}
	wl, err := guard.Compile(guard.WhitelistFile{}, guard.WhitelistFile{}, "")
	if err != nil {
		t.Fatal(err)
	}
	g := guard.NewGuard(wl, gfe)

	// Build a minimal agent with the spy tool and guard.
	turn1 := []common.StreamEvent{
		{Kind: common.EventToolCallDone, ToolCallID: "tc1", ToolName: "spy", InputJSON: `{}`},
		{Kind: common.EventTurnDone, StopReason: "tool_use"},
	}
	turn2 := []common.StreamEvent{
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{turn1, turn2}}

	a, sess := newTestAgentWithGuard(t, client, fe, g, reg)

	if err := a.Run(context.Background(), sess); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if capturedCtx == nil {
		t.Fatal("spy tool was not executed")
	}
	got := tools.SudoPasswordFrom(capturedCtx)
	if got != "secret123" {
		t.Errorf("SudoPasswordFrom = %q, want %q", got, "secret123")
	}
}

// TestRun_UnknownTool verifies that a tool_use for an unregistered tool name
// produces an IsError result but does not crash Run.
func TestRun_UnknownTool(t *testing.T) {
	turn1 := []common.StreamEvent{
		{Kind: common.EventToolCallDone, ToolCallID: "tc-unk", ToolName: "nonexistent", InputJSON: `{}`},
		{Kind: common.EventTurnDone, StopReason: "tool_use"},
	}
	turn2 := []common.StreamEvent{
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{turn1, turn2}}

	reg := tools.NewRegistry() // empty — nonexistent is not registered
	fe := &fakeRecorder{}
	g := compileYoloGuard(t)
	a, sess := newTestAgentWithGuard(t, client, fe, g, reg)

	if err := a.Run(context.Background(), sess); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var foundError bool
	for _, msg := range sess.History {
		if msg.Role == common.RoleTool {
			for _, b := range msg.Content {
				if b.IsError {
					foundError = true
				}
			}
		}
	}
	if !foundError {
		t.Errorf("expected IsError tool result for unknown tool")
	}
}
