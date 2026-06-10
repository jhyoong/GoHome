package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
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

func (r *recorderFrontend) AwaitUserInput(ctx context.Context, _ string) (string, error) {
	select {
	case s := <-r.input:
		return s, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestRunLoop_CancelMidTurn(t *testing.T) {
	bgCtx, bgCancel := context.WithCancel(context.Background())
	t.Cleanup(bgCancel)

	fe := &recorderFrontend{input: make(chan string, 1)}

	wl, err := guard.Compile(guard.WhitelistFile{}, guard.WhitelistFile{}, "")
	if err != nil {
		t.Fatalf("guard compile: %v", err)
	}
	g := guard.NewGuard(wl, nil)
	g.SetYolo(true)

	sess := session.NewSession("test-cancel", t.TempDir(), "model", "ep")
	writerPath := t.TempDir() + "/test.jsonl"
	writer, err := session.OpenWriter(writerPath)
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	state := agent.NewSessionState(sess, writer)

	a := &agent.Agent{
		Client:   &blockingClient{bgCtx: bgCtx},
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
	g := guard.NewGuard(wl, nil)
	g.SetYolo(true)

	sess := session.NewSession("test-race", t.TempDir(), "model", "ep")
	writerPath := t.TempDir() + "/test.jsonl"
	writer, err := session.OpenWriter(writerPath)
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	state := agent.NewSessionState(sess, writer)
	t.Cleanup(func() { _ = state.Writer().Close() })

	a := &agent.Agent{
		Client:   &blockingClient{bgCtx: bgCtx},
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

	queued, err := state.Swap("new swapped", func() (*session.Session, *session.Writer, error) {
		newW, err := session.OpenWriter(newWriterPath)
		if err != nil {
			return nil, nil, err
		}
		return newSess, newW, nil
	})
	if err != nil {
		t.Fatalf("swap error: %v", err)
	}
	if !queued {
		t.Fatal("expected swap to be queued while turn is in flight")
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
