package tui_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/jhyoong/GoHome/gohome/internal/tui"
)

func TestHelpOverlay_CtrlH_Opens(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() {
		_ = tm.Quit()
	})

	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlH})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Keyboard shortcuts")) &&
			bytes.Contains(out, []byte("Ctrl+H"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

func TestHelpOverlay_Esc_Closes(t *testing.T) {
	m := newSized()
	m.OpenHelpOverlay()

	if !m.ShowHelp() {
		t.Fatal("expected ShowHelp to be true after OpenHelpOverlay")
	}

	m = apply(m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.ShowHelp() {
		t.Fatal("expected ShowHelp to be false after Esc")
	}
}

func TestHelpOverlay_BlocksOtherKeys(t *testing.T) {
	m := newSized()
	m.OpenHelpOverlay()

	m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})

	if !m.ShowHelp() {
		t.Fatal("expected help overlay to remain open after non-Esc keys")
	}
}

func TestHelpOverlay_ScrollDown(t *testing.T) {
	// Use a small terminal so scrolling is needed.
	m := tui.New(nil, "")
	m = apply(m, tea.WindowSizeMsg{Width: 80, Height: 10})
	m.OpenHelpOverlay()

	view := m.View()
	if !strings.Contains(view, "Keyboard shortcuts") {
		t.Fatal("expected top of help content before scrolling")
	}

	// Scroll down past the first section.
	for i := 0; i < 8; i++ {
		m = apply(m, tea.KeyMsg{Type: tea.KeyDown})
	}

	view = m.View()
	if strings.Contains(view, "Keyboard shortcuts") {
		t.Fatal("expected 'Keyboard shortcuts' to be scrolled out of view")
	}
}

func TestHelpOverlay_ScrollUp(t *testing.T) {
	m := tui.New(nil, "")
	m = apply(m, tea.WindowSizeMsg{Width: 80, Height: 10})
	m.OpenHelpOverlay()

	// Scroll down then back up.
	for i := 0; i < 5; i++ {
		m = apply(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	for i := 0; i < 5; i++ {
		m = apply(m, tea.KeyMsg{Type: tea.KeyUp})
	}

	view := m.View()
	if !strings.Contains(view, "Keyboard shortcuts") {
		t.Fatal("expected to scroll back to top")
	}
}

func TestHelpOverlay_PgDownPgUp(t *testing.T) {
	m := tui.New(nil, "")
	m = apply(m, tea.WindowSizeMsg{Width: 80, Height: 10})
	m.OpenHelpOverlay()

	m = apply(m, tea.KeyMsg{Type: tea.KeyPgDown})

	view := m.View()
	if strings.Contains(view, "Keyboard shortcuts") {
		t.Fatal("expected PgDown to scroll past top")
	}

	m = apply(m, tea.KeyMsg{Type: tea.KeyPgUp})

	view = m.View()
	if !strings.Contains(view, "Keyboard shortcuts") {
		t.Fatal("expected PgUp to scroll back to top")
	}
}

func TestHelpOverlay_EndIndicator_WhenFits(t *testing.T) {
	// Large terminal where all content fits.
	m := tui.New(nil, "")
	m = apply(m, tea.WindowSizeMsg{Width: 80, Height: 60})
	m.OpenHelpOverlay()

	view := m.View()
	if !strings.Contains(view, "--END--") {
		t.Fatal("expected --END-- when all content fits")
	}
}

func TestHelpOverlay_ShowsCopyKeybinding(t *testing.T) {
	m := newSized()
	m.OpenHelpOverlay()

	view := m.View()
	if !strings.Contains(view, "Copy entry to clipboard") {
		t.Fatal("expected copy keybinding in help overlay")
	}
}

func TestHelpOverlay_EndIndicator_WhenScrolledToBottom(t *testing.T) {
	m := tui.New(nil, "")
	m = apply(m, tea.WindowSizeMsg{Width: 80, Height: 10})
	m.OpenHelpOverlay()

	// Scroll to the very bottom.
	for i := 0; i < 50; i++ {
		m = apply(m, tea.KeyMsg{Type: tea.KeyDown})
	}

	view := m.View()
	if !strings.Contains(view, "--END--") {
		t.Fatal("expected --END-- when scrolled to bottom")
	}
}

func TestHandleKeyMsg_DelegatesToActiveModal(t *testing.T) {
	m := newSized()
	m.OpenHelpOverlay()

	// Type a character while help is open — should NOT reach the editor.
	m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})

	if !m.ShowHelp() {
		t.Fatal("expected help overlay to remain open")
	}

	// Esc closes the modal, falling through to normal mode.
	m = apply(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.ShowHelp() {
		t.Fatal("expected help overlay to close on Esc")
	}

	// Now typing reaches the editor — it should appear in View.
	m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	view := m.View()
	if !strings.Contains(view, "x") {
		t.Fatal("expected 'x' in view after modal closed — key should reach editor")
	}
}
