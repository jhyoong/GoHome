package tui_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/charmbracelet/x/exp/teatest"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/tui"
)

// TestContextWarning80 drives a usage event above 80% of a small context window
// and asserts the 80% warning text appears.
func TestContextWarning80(t *testing.T) {
	m := tui.New(nil)
	// Set a small context window so we can easily exceed 80%.
	m.SetContextWindow(1000)

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	// Wait for initial render.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Session:"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Drive usage to 85% of 1000 = 850 tokens.
	tm.Send(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventUsageUpdated,
		SessionID: "main",
		Usage: &common.Usage{
			InputTokens:  600,
			OutputTokens: 250, // total = 850, 85% of 1000
		},
	}})

	// Assert the 80% warning appears in the notification line.
	var accum []byte
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		accum = append(accum, out...)
		return bytes.Contains(accum, []byte("80%"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

// TestContextWarning95 drives usage above 95% and asserts the 95% warning.
func TestContextWarning95(t *testing.T) {
	m := tui.New(nil)
	m.SetContextWindow(1000)

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Session:"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// First drive past 80% so both warnings fire cleanly.
	tm.Send(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventUsageUpdated,
		SessionID: "main",
		Usage: &common.Usage{
			InputTokens:  600,
			OutputTokens: 250, // 85%
		},
	}})

	// Now drive past 95% (960 tokens).
	tm.Send(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventUsageUpdated,
		SessionID: "main",
		Usage: &common.Usage{
			InputTokens:  700,
			OutputTokens: 260, // total = 960, 96%
		},
	}})

	// The 95% warning should supersede the 80% warning.
	var accum []byte
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		accum = append(accum, out...)
		return bytes.Contains(accum, []byte("near limit"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
