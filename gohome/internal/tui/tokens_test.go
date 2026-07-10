package tui_test

import (
	"bytes"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/tui"
)

// TestTokensOverlayShowsUsage opens the /tokens overlay and asserts the usage
// breakdown is rendered (including total); then presses Esc and asserts it closes.
func TestTokensOverlayShowsUsage(t *testing.T) {
	m := tui.New(nil, "")
	m.SetContextWindow(10000)

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	// Wait for initial render.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Session:"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Inject usage via an event.
	tm.Send(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventUsageUpdated,
		SessionID: "main",
		Usage: &common.Usage{
			InputTokens:      1234,
			OutputTokens:     567,
			CacheReadTokens:  89,
			CacheWriteTokens: 10,
		},
	}})

	// Open the /tokens overlay.
	tm.Type("/tokens")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert overlay shows "Input tokens" AND the total (1234+567=1801) in one pass.
	// A single WaitFor accumulates from where the previous one left off.
	var accumOut []byte
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		accumOut = append(accumOut, out...)
		return bytes.Contains(accumOut, []byte("Input tokens")) &&
			bytes.Contains(accumOut, []byte("1801"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Press Esc to close overlay.
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})

	// After Esc, the editor border must reappear in new output.
	var accumOut2 []byte
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		accumOut2 = append(accumOut2, out...)
		return bytes.Contains(accumOut2, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
