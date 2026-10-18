package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// blockingClient returns a channel that emits one text delta and then blocks
// indefinitely (never sends EventTurnDone). The channel is only closed when
// the test ends via the background context stored in the struct — NOT when
// the run context is cancelled. This forces Turn to see ctx.Done() rather
// than a channel close.
type blockingClient struct {
	firstDelta string
	bgCtx      context.Context // lifetime tied to the test, not the run
}

func (c *blockingClient) Stream(_ context.Context, _ common.Request) (<-chan common.StreamEvent, error) {
	ch := make(chan common.StreamEvent, 1)
	// Send one delta immediately so we know the turn started.
	ch <- common.StreamEvent{Kind: common.EventTextDelta, TextDelta: c.firstDelta}
	// Close the channel only when the background (test-lifetime) context ends,
	// not when the run context is cancelled. This guarantees that Turn's select
	// hits ctx.Done() first.
	go func() {
		<-c.bgCtx.Done()
		close(ch)
	}()
	return ch, nil
}

// TestRun_CancelMidTurn verifies that:
//   - Run returns context.Canceled when the context is cancelled mid-turn.
//   - The Frontend receives EventTurnDone with StopReason "cancelled".
//
// Run is NOT responsible for emitting session_end — that is the writer owner's
// job (cmd/gohome/main.go for the parent, agent.Spawn for child writers).
func TestRun_CancelMidTurn(t *testing.T) {
	bgCtx, bgCancel := context.WithCancel(context.Background())
	t.Cleanup(bgCancel) // ensure the client goroutine is cleaned up when the test ends
	client := &blockingClient{firstDelta: "partial", bgCtx: bgCtx}

	sess := session.NewSession("sess-cancel", t.TempDir(), "model", "anthropic")
	fe := &fakeRecorder{}

	wl, err := guardCompileEmpty(t)
	if err != nil {
		t.Fatalf("guard compile: %v", err)
	}
	g := guardNewYolo(wl)

	a := &Agent{
		Tools:    tools.NewRegistry(),
		Guard:    g,
		Frontend: fe,
		State:    NewSessionState(sess, nil, client), // Run never emits session_end; no writer needed here
		System:   "system",
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay — the blocking client will be in mid-stream.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err = a.Run(ctx, sess)
	if err != context.Canceled {
		t.Errorf("Run error: got %v, want context.Canceled", err)
	}

	// Frontend must have seen a turn_done with StopReason "cancelled".
	var sawCancelledTurnDone bool
	for _, ev := range fe.events {
		if ev.Kind == EventTurnDone && ev.StopReason == "cancelled" {
			sawCancelledTurnDone = true
		}
	}
	if !sawCancelledTurnDone {
		t.Errorf("frontend did not receive EventTurnDone{StopReason:'cancelled'}; events: %+v", fe.events)
	}
}

// guardCompileEmpty is a local helper to avoid importing guard test helpers.
func guardCompileEmpty(t *testing.T) (*guard.Whitelist, error) {
	t.Helper()
	return guard.Compile(guard.WhitelistFile{}, guard.WhitelistFile{}, "")
}

func guardNewYolo(wl *guard.Whitelist) *guard.Guard {
	g := guard.NewGuard(wl, nil, nil)
	g.SetYolo(true)
	return g
}

// cancellingTool cancels the run context when executed and counts its runs.
type cancellingTool struct {
	cancel context.CancelFunc
	runs   int
}

func (c *cancellingTool) Name() string                 { return "write" }
func (c *cancellingTool) Description() string          { return "cancelling tool" }
func (c *cancellingTool) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (c *cancellingTool) Execute(_ context.Context, _ json.RawMessage, _ tools.ProgressSink) (tools.Result, error) {
	c.runs++
	c.cancel()
	return tools.Result{Content: "written"}, nil
}

// TestRun_CancelBetweenToolCalls verifies that a cancel during the first of
// two whitelisted calls stops the second one. Both calls get a result so the
// history stays valid, and Run returns context.Canceled.
func TestRun_CancelBetweenToolCalls(t *testing.T) {
	turn1 := []common.StreamEvent{
		{Kind: common.EventToolCallDone, ToolCallID: "tc1", ToolName: "write", InputJSON: `{}`},
		{Kind: common.EventToolCallDone, ToolCallID: "tc2", ToolName: "write", InputJSON: `{}`},
		{Kind: common.EventTurnDone, StopReason: "tool_use"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{turn1}}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	tool := &cancellingTool{cancel: cancel}
	reg := tools.NewRegistry()
	reg.Register(tool)

	wl, err := guard.Compile(guard.WhitelistFile{Tools: []string{"write"}}, guard.WhitelistFile{}, "")
	if err != nil {
		t.Fatalf("guard.Compile: %v", err)
	}
	fe := &fakeRecorder{}
	a, sess := newTestAgentWithGuard(t, client, fe, guard.NewGuard(wl, nil, nil), reg)

	if err := a.Run(ctx, sess); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error: got %v, want context.Canceled", err)
	}
	if tool.runs != 1 {
		t.Errorf("tool runs: got %d, want 1", tool.runs)
	}
	if client.callCount != 1 {
		t.Errorf("Stream calls: got %d, want 1", client.callCount)
	}

	last := sess.History[len(sess.History)-1]
	if last.Role != common.RoleTool || len(last.Content) != 2 {
		t.Fatalf("last message: got role %v with %d blocks, want RoleTool with 2", last.Role, len(last.Content))
	}
	if got := last.Content[0]; got.ToolUseID != "tc1" || got.ResultText != "written" || got.IsError {
		t.Errorf("tc1 result: %+v", got)
	}
	if got := last.Content[1]; got.ToolUseID != "tc2" || got.ResultText != "skipped: cancelled" || !got.IsError {
		t.Errorf("tc2 result: %+v", got)
	}

	if err := a.State.Writer().Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(sess.CWD(), "session.jsonl"))
	if err != nil {
		t.Fatalf("read jsonl: %v", err)
	}
	if got := countToolResultLines(t, string(raw), "tc2"); got != 1 {
		t.Errorf("tool_result lines for tc2 = %d, want 1\n%s", got, raw)
	}
}

// cancelOnCompactClient returns turn1 on the first call. On the second call
// (the compaction request) it cancels the run context and returns a closed
// stream, as a cancelled HTTP stream would.
type cancelOnCompactClient struct {
	turn1     []common.StreamEvent
	cancel    context.CancelFunc
	callCount int
}

func (c *cancelOnCompactClient) Stream(_ context.Context, _ common.Request) (<-chan common.StreamEvent, error) {
	c.callCount++
	if c.callCount == 1 {
		ch := make(chan common.StreamEvent, len(c.turn1))
		for _, ev := range c.turn1 {
			ch <- ev
		}
		close(ch)
		return ch, nil
	}
	c.cancel()
	ch := make(chan common.StreamEvent)
	close(ch)
	return ch, nil
}

// TestRun_CancelDuringCompaction verifies that Run stops when the context is
// cancelled during auto-compaction (FOLLOWUP #36): no tool runs, the pending
// tool call gets a result, and Run returns context.Canceled.
func TestRun_CancelDuringCompaction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	client := &cancelOnCompactClient{
		turn1: []common.StreamEvent{
			{Kind: common.EventToolCallDone, ToolCallID: "tc1", ToolName: "write", InputJSON: `{}`},
			{Kind: common.EventTurnDone, StopReason: "tool_use", Usage: &common.Usage{InputTokens: 900}},
		},
		cancel: cancel,
	}

	tool := &cancellingTool{cancel: cancel}
	reg := tools.NewRegistry()
	reg.Register(tool)
	fe := &fakeRecorder{}
	a, sess := newTestAgentWithGuard(t, client, fe, compileYoloGuard(t), reg)
	a.CompactCfg = CompactConfig{Enabled: true, Mode: "percentage", TriggerPct: 0.5, ContextWindow: 1000}

	// Enough history for compact to send a summary request.
	for i := 0; i < 4; i++ {
		sess.History = append(sess.History,
			common.Message{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "q"}}},
			common.Message{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "a"}}},
		)
	}
	sess.History = append(sess.History,
		common.Message{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "go"}}})

	if err := a.Run(ctx, sess); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error: got %v, want context.Canceled", err)
	}
	if client.callCount != 2 {
		t.Fatalf("Stream calls: got %d, want 2 (turn + compaction)", client.callCount)
	}
	if tool.runs != 0 {
		t.Errorf("tool runs: got %d, want 0", tool.runs)
	}
	last := sess.History[len(sess.History)-1]
	if last.Role != common.RoleTool || len(last.Content) != 1 || last.Content[0].ResultText != "skipped: cancelled" {
		t.Errorf("last message: %+v, want one skipped: cancelled tool result", last)
	}
}
