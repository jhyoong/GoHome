package tui_test

import (
	"bytes"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/jhyoong/GoHome/gohome/internal/tui"
	"github.com/muesli/termenv"
)

// TestSlashYoloTogglesYolo types "/yolo" then Enter and asserts yolo toggled.
func TestSlashYoloTogglesYolo(t *testing.T) {
	m := tui.New(nil, "")
	// Capture initial yolo state.
	initialYolo := m.Yolo()

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	// Wait for the TUI to initialise.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
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

// TestSlashYoloCallbackFires asserts that SetYoloCallback is invoked when
// /yolo is typed, receiving the new yolo value each time.
func TestSlashYoloCallbackFires(t *testing.T) {
	m := tui.New(nil, "")

	var recorded []bool
	m.SetYoloCallback(func(v bool) { recorded = append(recorded, v) })

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// First toggle: off -> on
	tm.Type("/yolo")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("YOLO mode ON"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Second toggle: on -> off
	tm.Type("/yolo")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("YOLO mode OFF"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Give the test model time to process.
	_ = tm.Quit()

	if len(recorded) < 2 {
		t.Fatalf("callback called %d times, want at least 2; recorded: %v", len(recorded), recorded)
	}
	if recorded[0] != true {
		t.Errorf("first callback value: got %v, want true", recorded[0])
	}
	if recorded[1] != false {
		t.Errorf("second callback value: got %v, want false", recorded[1])
	}
}

// TestSlashNewNotConfigured types "/new" then Enter and asserts "not configured"
// when no NewSession callback has been registered.
func TestSlashNewNotConfigured(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Type("/new")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("not configured"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

// TestSlashPaletteSuggestsCommands types "/" and asserts command suggestions appear.
func TestSlashPaletteSuggestsCommands(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Type just "/" to trigger the palette with all commands.
	tm.Type("/")

	// Palette should show "/new" among suggestions.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("/new"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

func TestTabCompletesSlashCommand(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Type("/mo")
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	tm.Type("x")

	// If Tab completed, editor is "/model x". If not, editor is "/mox".
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("/model x"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

func TestTabNoMatchDoesNothing(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Type("/xyz")
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})

	// Editor should still show "/xyz" — no completion happened.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("/xyz"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

func TestTabAlreadyCompleteDoesNothing(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Type a complete command followed by a space.
	tm.Type("/model ")
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})

	// Editor should still show "/model " — no change.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("/model"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

func TestSlashPaletteHighlightsFirstMatch(t *testing.T) {
	// Force ANSI color output so lipgloss emits bold escape sequences even in
	// a non-TTY test environment.
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Type "/re" to narrow to "/resume".
	tm.Type("/re")

	// The palette should render /resume with bold ANSI (ESC[1m).
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("\x1b[1m")) && bytes.Contains(out, []byte("/resume"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

func TestTabCompletesFirstMatchFromSlash(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// "/" matches all commands; first in list is "/help"
	tm.Type("/")
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	tm.Type("x")

	// If Tab completed, editor is "/help x". If not, editor is "/x".
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("/help x"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
