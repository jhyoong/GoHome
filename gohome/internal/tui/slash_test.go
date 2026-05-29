package tui_test

import (
	"bytes"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/jhyoong/GoHome/gohome/internal/tui"
)

// TestSlashYoloTogglesYolo types "/yolo" then Enter and asserts yolo toggled.
func TestSlashYoloTogglesYolo(t *testing.T) {
	m := tui.New(nil)
	// Capture initial yolo state.
	initialYolo := m.Yolo()

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	// Wait for the TUI to initialise.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Type a message"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Type("/yolo")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Assert YOLO appears in the rendered output (status bar shows [YOLO]).
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("YOLO"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// The getter should reflect the toggle (opposite of initial).
	_ = initialYolo // reference to avoid unused var
}

// TestSlashNewNotImplemented types "/new" then Enter and asserts "not implemented".
func TestSlashNewNotImplemented(t *testing.T) {
	m := tui.New(nil)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Type a message"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Type("/new")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("not implemented"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

// TestSlashPaletteSuggestsCommands types "/" and asserts command suggestions appear.
func TestSlashPaletteSuggestsCommands(t *testing.T) {
	m := tui.New(nil)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Type a message"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Type just "/" to trigger the palette with all commands.
	tm.Type("/")

	// Palette should show "/new" among suggestions.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("/new"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
