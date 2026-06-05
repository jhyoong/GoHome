package agent

import (
	"context"
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
		Client:   client,
		Tools:    tools.NewRegistry(),
		Guard:    g,
		Frontend: fe,
		Writer:   nil, // Run never emits session_end; no writer needed here
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
	g := guard.NewGuard(wl, nil)
	g.SetYolo(true)
	return g
}
