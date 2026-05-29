package tui_test

// TestSnapshots provides a golden-file snapshot suite for the TUI.
// Run with -update to regenerate golden files:
//
//	go test ./gohome/internal/tui/ -run TestSnapshots -update
//
// Determinism: lipgloss.SetColorProfile(termenv.Ascii) is called in TestMain
// (see tui_test_main_test.go) so all colour codes are stripped, making
// terminal output stable across machines and colour profiles.
//
// Note on output capture: FinalOutput collects all bytes written to the
// virtual terminal from program start until quit. The golden files include
// the raw terminal sequences (cursor movement, erase). With termenv.Ascii
// no colour-code sequences are emitted, keeping the golden files stable.

import (
	"io"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/tui"
)

const snapshotW = 80
const snapshotH = 24

// settle waits a short fixed duration for the TUI to process pending messages
// and re-render. This avoids draining the output buffer (unlike WaitFor).
func settle() {
	time.Sleep(80 * time.Millisecond)
}

// captureSnapshot quits tm and returns the full accumulated output.
func captureSnapshot(t *testing.T, tm *teatest.TestModel) []byte {
	t.Helper()
	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(tm.FinalOutput(t, teatest.WithFinalTimeout(3*time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSnapshots(t *testing.T) {
	// (a) Empty initial view.
	t.Run("empty_initial_view", func(t *testing.T) {
		m := tui.New(nil)
		tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(snapshotW, snapshotH))
		settle()
		out := captureSnapshot(t, tm)
		teatest.RequireEqualOutput(t, out)
	})

	// (b) After a single user message.
	t.Run("after_user_message", func(t *testing.T) {
		m := tui.New(nil)
		m.AddTimelineEntry("main", tui.TimelineEntry{Kind: "user", Text: "hello world"})
		tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(snapshotW, snapshotH))
		settle()
		out := captureSnapshot(t, tm)
		teatest.RequireEqualOutput(t, out)
	})

	// (c) After one assistant turn (token deltas + turn done).
	t.Run("after_assistant_turn", func(t *testing.T) {
		m := tui.New(nil)
		m.AddTimelineEntry("main", tui.TimelineEntry{Kind: "user", Text: "what is 2+2?"})
		m.AddTimelineEntry("main", tui.TimelineEntry{Kind: "assistant", Text: "The answer is 4."})
		tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(snapshotW, snapshotH))
		settle()
		out := captureSnapshot(t, tm)
		teatest.RequireEqualOutput(t, out)
	})

	// (d) With an approval prompt active.
	t.Run("with_approval_prompt", func(t *testing.T) {
		m := tui.New(nil)
		tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(snapshotW, snapshotH))
		reply := make(chan guard.ApprovalDecision, 1)
		tm.Send(tui.ApprovalReqMsg{
			Req: guard.ApprovalRequest{
				SessionID:        "main",
				Tool:             "bash",
				Input:            []byte(`{"command":"ls -la"}`),
				SuggestedPattern: "ls*",
			},
			Reply: reply,
		})
		settle()
		out := captureSnapshot(t, tm)
		teatest.RequireEqualOutput(t, out)
	})

	// (e) With a subagent in the session strip.
	t.Run("with_subagent_strip", func(t *testing.T) {
		m := tui.New(nil)
		tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(snapshotW, snapshotH))
		tm.Send(tui.AgentEventMsg{SessionID: "sub1", Ev: agent.Event{
			Kind:      agent.EventSessionStarted,
			SessionID: "sub1",
		}})
		settle()
		out := captureSnapshot(t, tm)
		teatest.RequireEqualOutput(t, out)
	})

	// (f) With the /tokens overlay open.
	t.Run("with_tokens_overlay", func(t *testing.T) {
		m := tui.New(nil)
		m.SetModelName("claude-3-5-sonnet")
		m.SetContextWindow(100000)
		tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(snapshotW, snapshotH))

		settle() // wait for initial render

		tm.Send(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
			Kind:      agent.EventUsageUpdated,
			SessionID: "main",
			Usage: &common.Usage{
				InputTokens:      5000,
				OutputTokens:     1000,
				CacheReadTokens:  200,
				CacheWriteTokens: 50,
			},
		}})
		settle() // wait for usage update to be processed

		tm.Type("/tokens")
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

		settle() // wait for overlay render
		settle() // extra settle for stability
		out := captureSnapshot(t, tm)
		teatest.RequireEqualOutput(t, out)
	})
}
