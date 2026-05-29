package tui_test

import (
	"bytes"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/tui"
)

// TestSessionStripShowsFocused verifies the strip renders when there is only
// the default "main" session.
func TestSessionStripShowsFocused(t *testing.T) {
	m := tui.New(nil)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 24))
	t.Cleanup(func() {
		_ = tm.Quit()
	})

	// The strip must show "Session:" and "main".
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Session:")) &&
			bytes.Contains(out, []byte("main"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

// TestFocusCyclingCtrlCloseBracket verifies that Ctrl+] moves focus to a
// sub-session registered via an EventSessionStarted message.
func TestFocusCyclingCtrlCloseBracket(t *testing.T) {
	m := tui.New(nil)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 24))
	t.Cleanup(func() {
		_ = tm.Quit()
	})

	// Register a sub-session via an agent event.
	tm.Send(tui.AgentEventMsg{
		SessionID: "sub-1",
		Ev: agent.Event{
			Kind:      agent.EventSessionStarted,
			SessionID: "sub-1",
		},
	})

	// Wait for the strip to reflect the new session chip.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("sub-1"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Send Ctrl+] to cycle focus forward (main -> sub-1).
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})

	// After focus changes, the focused session should be "sub-1".
	// The strip marks the focused chip with reverse style; we also expose
	// Focused() so we can assert directly via FinalModel.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		// We just verify the output still shows sub-1 after the key.
		return bytes.Contains(out, []byte("sub-1"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Quit and inspect FinalModel to assert focused == "sub-1".
	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}
	fm := tm.FinalModel(t, teatest.WithFinalTimeout(time.Second))
	tuiModel, ok := fm.(*tui.Model)
	if !ok {
		t.Fatalf("expected *tui.Model, got %T", fm)
	}
	if tuiModel.Focused() != "sub-1" {
		t.Errorf("expected focused session to be %q, got %q", "sub-1", tuiModel.Focused())
	}
}

// TestFocusCyclingCtrlOpenBracket verifies that Ctrl+[ moves focus backward.
func TestFocusCyclingCtrlOpenBracket(t *testing.T) {
	m := tui.New(nil)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 24))
	t.Cleanup(func() {
		_ = tm.Quit()
	})

	// Register two sub-sessions.
	tm.Send(tui.AgentEventMsg{
		SessionID: "sub-1",
		Ev:        agent.Event{Kind: agent.EventSessionStarted, SessionID: "sub-1"},
	})
	tm.Send(tui.AgentEventMsg{
		SessionID: "sub-2",
		Ev:        agent.Event{Kind: agent.EventSessionStarted, SessionID: "sub-2"},
	})

	// Wait for both to appear.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("sub-2"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Ctrl+[ from "main" wraps around to "sub-2" (last in order).
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlOpenBracket})

	// Quit and assert focus is "sub-2".
	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}
	fm := tm.FinalModel(t, teatest.WithFinalTimeout(time.Second))
	tuiModel, ok := fm.(*tui.Model)
	if !ok {
		t.Fatalf("expected *tui.Model, got %T", fm)
	}
	if tuiModel.Focused() != "sub-2" {
		t.Errorf("expected focused session to be %q after Ctrl+[, got %q", "sub-2", tuiModel.Focused())
	}
}
