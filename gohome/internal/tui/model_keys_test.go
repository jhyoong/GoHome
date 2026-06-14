package tui_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

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
