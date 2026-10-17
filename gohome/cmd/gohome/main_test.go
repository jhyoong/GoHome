package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// blockingClient sends one delta then blocks until its background context ends.
type blockingClient struct {
	bgCtx context.Context
}

func (c *blockingClient) Stream(_ context.Context, _ common.Request) (<-chan common.StreamEvent, error) {
	ch := make(chan common.StreamEvent, 1)
	ch <- common.StreamEvent{Kind: common.EventTextDelta, TextDelta: "partial"}
	go func() {
		<-c.bgCtx.Done()
		close(ch)
	}()
	return ch, nil
}

// recorderFrontend captures events and delivers input from a channel.
type recorderFrontend struct {
	mu     sync.Mutex
	events []agent.Event
	input  chan string
}

func (r *recorderFrontend) Emit(_ string, ev agent.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *recorderFrontend) RequestApproval(ctx context.Context, _ guard.ApprovalRequest) (guard.ApprovalDecision, error) {
	<-ctx.Done()
	return guard.ApprovalDecision{Outcome: guard.Deny}, ctx.Err()
}

func (r *recorderFrontend) AwaitUserInput(ctx context.Context) (string, error) {
	select {
	case s := <-r.input:
		return s, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (r *recorderFrontend) SteerCh() <-chan string {
	return nil
}

func TestRunLoop_CancelMidTurn(t *testing.T) {
	bgCtx, bgCancel := context.WithCancel(context.Background())
	t.Cleanup(bgCancel)

	fe := &recorderFrontend{input: make(chan string, 1)}

	wl, err := guard.Compile(guard.WhitelistFile{}, guard.WhitelistFile{}, "")
	if err != nil {
		t.Fatalf("guard compile: %v", err)
	}
	g := guard.NewGuard(wl, nil, nil)
	g.SetYolo(true)

	sess := session.NewSession("test-cancel", t.TempDir(), "model", "ep")
	writerPath := t.TempDir() + "/test.jsonl"
	writer, err := session.OpenWriter(writerPath)
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	client := &blockingClient{bgCtx: bgCtx}
	state := agent.NewSessionState(sess, writer, client)

	a := &agent.Agent{
		Tools:    tools.NewRegistry(),
		Guard:    g,
		Frontend: fe,
		State:    state,
		System:   "test",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		turnMu     sync.Mutex
		turnCancel context.CancelFunc
	)

	// Start runLoop in background.
	done := make(chan struct{})
	go func() {
		defer close(done)
		runLoop(ctx, a, fe, &turnMu, &turnCancel)
	}()

	// Send user input to start a turn.
	fe.input <- "hello"

	// Wait for the blocking client to emit a delta (turn is in flight).
	deadline := time.After(2 * time.Second)
	for {
		fe.mu.Lock()
		n := len(fe.events)
		fe.mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for first event")
		case <-time.After(5 * time.Millisecond):
		}
	}

	// Verify turnCancel is set.
	turnMu.Lock()
	if turnCancel == nil {
		t.Fatal("turnCancel should be set while turn is in flight")
	}
	// Cancel the turn (simulating CancelSession callback).
	turnCancel()
	turnCancel = nil
	turnMu.Unlock()

	// Wait for runLoop to process the cancellation and be ready for more input.
	// It should NOT exit — only the per-turn context was cancelled.
	time.Sleep(50 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("runLoop exited — it should continue after a per-turn cancel")
	default:
	}

	// Verify turnCancel was cleared by runLoop.
	turnMu.Lock()
	if turnCancel != nil {
		t.Error("turnCancel should be nil after turn completes")
	}
	turnMu.Unlock()

	// Verify frontend saw a cancelled turn_done.
	fe.mu.Lock()
	var sawCancelledDone bool
	for _, ev := range fe.events {
		if ev.Kind == agent.EventTurnDone && ev.StopReason == "cancelled" {
			sawCancelledDone = true
		}
	}
	fe.mu.Unlock()
	if !sawCancelledDone {
		t.Error("frontend did not receive EventTurnDone with StopReason 'cancelled'")
	}

	// Clean shutdown.
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runLoop did not exit after process context cancel")
	}
}

func TestConcurrentSwapAndRun(t *testing.T) {
	bgCtx, bgCancel := context.WithCancel(context.Background())
	t.Cleanup(bgCancel)

	fe := &recorderFrontend{input: make(chan string, 1)}

	wl, err := guard.Compile(guard.WhitelistFile{}, guard.WhitelistFile{}, "")
	if err != nil {
		t.Fatalf("guard compile: %v", err)
	}
	g := guard.NewGuard(wl, nil, nil)
	g.SetYolo(true)

	sess := session.NewSession("test-race", t.TempDir(), "model", "ep")
	writerPath := t.TempDir() + "/test.jsonl"
	writer, err := session.OpenWriter(writerPath)
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	client := &blockingClient{bgCtx: bgCtx}
	state := agent.NewSessionState(sess, writer, client)
	t.Cleanup(func() { _ = state.Writer().Close() })

	a := &agent.Agent{
		Tools:    tools.NewRegistry(),
		Guard:    g,
		Frontend: fe,
		State:    state,
		System:   "test",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		turnMu     sync.Mutex
		turnCancel context.CancelFunc
	)

	done := make(chan struct{})
	go func() {
		defer close(done)
		runLoop(ctx, a, fe, &turnMu, &turnCancel)
	}()

	// Start a turn.
	fe.input <- "hello"

	// Wait for turn to be in flight.
	deadline := time.After(2 * time.Second)
	for {
		fe.mu.Lock()
		n := len(fe.events)
		fe.mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for first event")
		case <-time.After(5 * time.Millisecond):
		}
	}

	// Queue a swap while the turn is in flight.
	newSess := session.NewSession("swapped", t.TempDir(), "model", "ep")
	newWriterPath := t.TempDir() + "/swapped.jsonl"

	err = state.Swap("new swapped", func(_ *session.Session, _ *session.Writer) (*session.Session, *session.Writer, error) {
		newW, err := session.OpenWriter(newWriterPath)
		if err != nil {
			return nil, nil, err
		}
		return newSess, newW, nil
	})
	if err != nil {
		t.Fatalf("swap error: %v", err)
	}

	// Session should still be the original while turn is in flight.
	if state.Session().ID != "test-race" {
		t.Errorf("session = %q, want test-race (swap is queued)", state.Session().ID)
	}

	// Cancel the turn to let it finish.
	turnMu.Lock()
	if turnCancel != nil {
		turnCancel()
	}
	turnMu.Unlock()

	// Wait for DrainPending to execute.
	time.Sleep(100 * time.Millisecond)

	// After the turn ends, DrainPending should have executed the swap.
	if state.Session().ID != "swapped" {
		t.Errorf("session = %q, want swapped (drain should have run)", state.Session().ID)
	}

	// Check that EventSessionSwapped was emitted.
	fe.mu.Lock()
	var sawSwapped bool
	for _, ev := range fe.events {
		if ev.Kind == agent.EventSessionSwapped && ev.SessionID == "swapped" {
			sawSwapped = true
		}
	}
	fe.mu.Unlock()
	if !sawSwapped {
		t.Error("frontend did not receive EventSessionSwapped")
	}

	// Clean shutdown.
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runLoop did not exit after process context cancel")
	}
}

func TestSetupLogging_OwnerOnlyPerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits not meaningful on Windows")
	}
	home := filepath.Join(t.TempDir(), ".gohome")
	f, err := setupLogging(home)
	if err != nil {
		t.Fatalf("setupLogging: %v", err)
	}
	defer func() { _ = f.Close() }()
	for _, d := range []string{home, filepath.Join(home, "logs")} {
		st, err := os.Stat(d)
		if err != nil {
			t.Fatal(err)
		}
		if got := st.Mode().Perm(); got != 0o700 {
			t.Errorf("%s mode = %o, want 700", d, got)
		}
	}
	st, err := os.Stat(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Mode().Perm(); got != 0o600 {
		t.Errorf("log file mode = %o, want 600", got)
	}
}

func TestSetupLogging_KeepsExistingDirPerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits not meaningful on Windows")
	}
	home := filepath.Join(t.TempDir(), ".gohome")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(home, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := setupLogging(home)
	if err != nil {
		t.Fatalf("setupLogging: %v", err)
	}
	defer func() { _ = f.Close() }()
	st, _ := os.Stat(home)
	if got := st.Mode().Perm(); got != 0o755 {
		t.Errorf("existing dir mode changed to %o", got)
	}
}

// writeSession writes a minimal session file with one user message.
func writeSession(t *testing.T, home, cwd string, start session.SessionStart, text string) {
	t.Helper()
	w, err := session.OpenWriter(session.SessionPath(home, cwd, start.ID, start.StartedAt))
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	w.Emit(start)
	w.Emit(session.UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: text}}})
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestPickResume_SkipsChildAndLoadsChildren reproduces audit item D2: the
// child starts after its parent, but --resume must pick the parent, and the
// parent's children must load for display.
func TestPickResume_SkipsChildAndLoadsChildren(t *testing.T) {
	home := t.TempDir()
	cwd := "/home/user/proj"
	now := time.Now().UTC()
	writeSession(t, home, cwd, session.SessionStart{ID: "p1", CWD: cwd, StartedAt: now.Add(-time.Minute)}, "parent")
	writeSession(t, home, cwd, session.SessionStart{ID: "p1-sub-1", ParentID: "p1", CWD: cwd, Depth: 1, StartedAt: now}, "child")

	sess, _, _, err := pickResume(home, cwd)
	if err != nil {
		t.Fatalf("pickResume: %v", err)
	}
	if sess == nil || sess.ID != "p1" {
		t.Fatalf("pickResume picked %+v, want p1", sess)
	}
	children := loadChildren(home, cwd, sess.ID)
	if len(children) != 1 || children[0].ID != "p1-sub-1" {
		t.Fatalf("loadChildren = %v, want [p1-sub-1]", children)
	}
}

// doneClient answers each request with one text delta and a clean turn end.
type doneClient struct{}

func (doneClient) Stream(_ context.Context, _ common.Request) (<-chan common.StreamEvent, error) {
	ch := make(chan common.StreamEvent, 2)
	ch <- common.StreamEvent{Kind: common.EventTextDelta, TextDelta: "ok"}
	ch <- common.StreamEvent{Kind: common.EventTurnDone, StopReason: "end_turn"}
	close(ch)
	return ch, nil
}

func TestRunLoop_WriteFailureIsNoticeOnce(t *testing.T) {
	fe := &recorderFrontend{input: make(chan string, 1)}

	wl, err := guard.Compile(guard.WhitelistFile{}, guard.WhitelistFile{}, "")
	if err != nil {
		t.Fatalf("guard compile: %v", err)
	}
	g := guard.NewGuard(wl, nil, nil)
	g.SetYolo(true)

	sess := session.NewSession("test-write-fail", t.TempDir(), "model", "ep")
	writer, err := session.OpenWriter(t.TempDir() + "/test.jsonl")
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	// An unknown event type makes the background writer record an error.
	writer.Emit(42)
	deadline := time.After(2 * time.Second)
	for writer.Err() == nil {
		select {
		case <-deadline:
			t.Fatal("writer error was not recorded")
		case <-time.After(5 * time.Millisecond):
		}
	}

	a := &agent.Agent{
		Tools:    tools.NewRegistry(),
		Guard:    g,
		Frontend: fe,
		State:    agent.NewSessionState(sess, writer, doneClient{}),
		System:   "test",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var (
		turnMu     sync.Mutex
		turnCancel context.CancelFunc
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runLoop(ctx, a, fe, &turnMu, &turnCancel)
	}()

	// Two runs on the same writer: the notice must appear once.
	countRunDone := func() int {
		fe.mu.Lock()
		defer fe.mu.Unlock()
		n := 0
		for _, ev := range fe.events {
			if ev.Kind == agent.EventRunDone {
				n++
			}
		}
		return n
	}
	for run := 1; run <= 2; run++ {
		fe.input <- "hello"
		deadline := time.After(2 * time.Second)
		for countRunDone() < run {
			select {
			case <-deadline:
				t.Fatalf("run %d did not finish", run)
			case <-time.After(5 * time.Millisecond):
			}
		}
	}
	cancel()
	<-done

	fe.mu.Lock()
	defer fe.mu.Unlock()
	notices := 0
	for _, ev := range fe.events {
		if ev.Kind == agent.EventError {
			t.Errorf("got EventError %v; write failure must be a notice", ev.Err)
		}
		if ev.Kind == agent.EventNotice && strings.Contains(ev.Notice, "session not saved") {
			notices++
		}
	}
	if notices != 1 {
		t.Errorf("session not saved notices = %d, want 1", notices)
	}
}

// toolCallClient answers each request with one tool call.
type toolCallClient struct{}

func (toolCallClient) Stream(_ context.Context, _ common.Request) (<-chan common.StreamEvent, error) {
	ch := make(chan common.StreamEvent, 2)
	ch <- common.StreamEvent{Kind: common.EventToolCallDone, ToolCallID: "tc1", ToolName: "shell", InputJSON: `{}`}
	ch <- common.StreamEvent{Kind: common.EventTurnDone, StopReason: "tool_use"}
	close(ch)
	return ch, nil
}

// denyFrontend denies every approval after calling onApproval.
type denyFrontend struct {
	recorderFrontend
	onApproval func()
}

func (d *denyFrontend) RequestApproval(_ context.Context, _ guard.ApprovalRequest) (guard.ApprovalDecision, error) {
	d.onApproval()
	return guard.ApprovalDecision{Outcome: guard.Deny}, nil
}

// TestRunLoop_DeniedRunAppliesPendingSwap verifies that a session swap
// queued during a run is applied when the run ends with a tool denial.
func TestRunLoop_DeniedRunAppliesPendingSwap(t *testing.T) {
	wl, err := guard.Compile(guard.WhitelistFile{}, guard.WhitelistFile{}, "")
	if err != nil {
		t.Fatalf("guard compile: %v", err)
	}

	sess := session.NewSession("test-deny", t.TempDir(), "model", "ep")
	writer, err := session.OpenWriter(t.TempDir() + "/test.jsonl")
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	state := agent.NewSessionState(sess, writer, toolCallClient{})
	t.Cleanup(func() { _ = state.Writer().Close() })

	newSess := session.NewSession("swapped", t.TempDir(), "model", "ep")
	newWriterPath := t.TempDir() + "/swapped.jsonl"
	fe := &denyFrontend{recorderFrontend: recorderFrontend{input: make(chan string, 1)}}
	fe.onApproval = func() {
		// The agent is busy here, so the swap is queued.
		_ = state.Swap("new swapped", func(_ *session.Session, oldWriter *session.Writer) (*session.Session, *session.Writer, error) {
			_ = oldWriter.Close()
			newW, err := session.OpenWriter(newWriterPath)
			if err != nil {
				return nil, nil, err
			}
			return newSess, newW, nil
		})
	}

	a := &agent.Agent{
		Tools:    tools.NewRegistry(),
		Guard:    guard.NewGuard(wl, fe, nil),
		Frontend: fe,
		State:    state,
		System:   "test",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var (
		turnMu     sync.Mutex
		turnCancel context.CancelFunc
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runLoop(ctx, a, fe, &turnMu, &turnCancel)
	}()

	fe.input <- "hello"

	deadline := time.After(2 * time.Second)
	for state.Session().ID != "swapped" {
		select {
		case <-deadline:
			t.Fatalf("session = %q, want swapped (denied run must drain the pending swap)", state.Session().ID)
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runLoop did not exit after process context cancel")
	}
}

// reqClient records the last request and answers with a clean turn end.
type reqClient struct {
	mu  sync.Mutex
	req common.Request
}

func (c *reqClient) Stream(_ context.Context, req common.Request) (<-chan common.StreamEvent, error) {
	c.mu.Lock()
	c.req = req
	c.mu.Unlock()
	ch := make(chan common.StreamEvent, 1)
	ch <- common.StreamEvent{Kind: common.EventTurnDone, StopReason: "end_turn"}
	close(ch)
	return ch, nil
}

// TestApplyModelConfig_DefaultsAndCurrentModel applies a config with no
// maxTokens, thinking or thinkingBudget to a resumed session saved with an
// old model. maxTokens gets the startup default, thinking is adaptive, and the
// turn sends the new model.
func TestApplyModelConfig_DefaultsAndCurrentModel(t *testing.T) {
	sess := session.NewSession("resumed", t.TempDir(), "old-model", "old-cfg")
	state := agent.NewSessionState(sess, nil, nil)
	client := &reqClient{}

	ctxWin, err := applyModelConfig(state, "fast", config.ModelConfig{ModelName: "new-model"}, client)
	if err != nil {
		t.Fatalf("applyModelConfig: %v", err)
	}

	if ctxWin != config.DefaultContextWindow {
		t.Errorf("context window = %d, want %d", ctxWin, config.DefaultContextWindow)
	}
	ms := state.ModelSettings()
	if ms.MaxTokens != config.DefaultMaxTokens {
		t.Errorf("MaxTokens = %d, want %d", ms.MaxTokens, config.DefaultMaxTokens)
	}
	if ms.Thinking != common.ThinkingModeAdaptive || ms.ThinkingBudget != 0 {
		t.Errorf("thinking = %q/%d, want adaptive/0", ms.Thinking, ms.ThinkingBudget)
	}
	if sess.Model != "new-model" || sess.ModelConfig != "fast" {
		t.Errorf("session model = %q/%q, want new-model/fast", sess.Model, sess.ModelConfig)
	}

	a := &agent.Agent{Tools: tools.NewRegistry(), Frontend: &recorderFrontend{}, State: state}
	if _, _, err := a.Turn(context.Background(), sess); err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if client.req.Model != "new-model" {
		t.Errorf("request model = %q, want new-model", client.req.Model)
	}
	if client.req.MaxTokens != config.DefaultMaxTokens || client.req.Thinking != common.ThinkingModeAdaptive {
		t.Errorf("request limits = %d/%q, want %d/adaptive", client.req.MaxTokens, client.req.Thinking,
			config.DefaultMaxTokens)
	}
}

// TestApplyModelConfig_BudgetMode sends the configured budget and effort.
func TestApplyModelConfig_BudgetMode(t *testing.T) {
	sess := session.NewSession("s", t.TempDir(), "m", "c")
	state := agent.NewSessionState(sess, nil, nil)
	cfg := config.ModelConfig{ModelName: "m", MaxTokens: 8192, ThinkingBudget: 4096, ReasoningEffort: "high"}

	if _, err := applyModelConfig(state, "c", cfg, &reqClient{}); err != nil {
		t.Fatalf("applyModelConfig: %v", err)
	}
	ms := state.ModelSettings()
	if ms.Thinking != common.ThinkingModeBudget || ms.ThinkingBudget != 4096 || ms.ReasoningEffort != "high" {
		t.Errorf("settings = %q/%d/%q, want budget/4096/high", ms.Thinking, ms.ThinkingBudget, ms.ReasoningEffort)
	}
}

// TestApplyModelConfig_InvalidBudgetKeepsCurrentModel rejects a budget that
// does not fit below maxTokens and leaves the active model unchanged. This is
// the path /model uses.
func TestApplyModelConfig_InvalidBudgetKeepsCurrentModel(t *testing.T) {
	sess := session.NewSession("s", t.TempDir(), "old-model", "old")
	state := agent.NewSessionState(sess, nil, nil)
	oldClient := &reqClient{}
	if _, err := applyModelConfig(state, "old", config.ModelConfig{ModelName: "old-model"}, oldClient); err != nil {
		t.Fatalf("applyModelConfig old: %v", err)
	}

	bad := config.ModelConfig{Wire: config.WireAnthropic, ModelName: "new-model", Thinking: "budget", ThinkingBudget: 8192, MaxTokens: 4096}
	_, err := applyModelConfig(state, "bad", bad, &reqClient{})
	if err == nil {
		t.Fatal("expected error for budget >= maxTokens")
	}
	if !strings.Contains(err.Error(), `"bad"`) {
		t.Errorf("error %q should name the model config", err)
	}
	if ms := state.ModelSettings(); ms.ConfigName != "old" || ms.Name != "old-model" {
		t.Errorf("model settings changed to %q/%q, want old/old-model", ms.ConfigName, ms.Name)
	}
	if state.Client() != oldClient {
		t.Error("client changed after a rejected config")
	}
}
