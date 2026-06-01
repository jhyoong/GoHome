package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestIsPasteMsgTrue(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello"), Paste: true}
	if !IsPasteMsg(msg) {
		t.Error("IsPasteMsg should return true for paste message")
	}
}

func TestIsPasteMsgFalse(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello"), Paste: false}
	if IsPasteMsg(msg) {
		t.Error("IsPasteMsg should return false for non-paste message")
	}
}

func TestPasteIntegrationWithEditor(t *testing.T) {
	e := NewEditor(80, 24)
	// Simulate what happens when a paste KeyMsg arrives
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("pasted\ntext"), Paste: true}
	e.HandleInput(msg)
	want := "pasted\ntext"
	if e.Value() != want {
		t.Errorf("after paste, Value() = %q, want %q", e.Value(), want)
	}
}

func TestPasteMultiLinePreservesExisting(t *testing.T) {
	e := NewEditor(80, 24)
	e.InsertRune('a')
	e.InsertRune('b')
	// Simulate paste at cursor position (after "ab")
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X\nY\nZ"), Paste: true}
	e.HandleInput(msg)
	want := "abX\nY\nZ"
	if e.Value() != want {
		t.Errorf("after paste, Value() = %q, want %q", e.Value(), want)
	}
}

func TestPasteStripsCarriageReturn(t *testing.T) {
	e := NewEditor(80, 24)
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line1\r\nline2"), Paste: true}
	e.HandleInput(msg)
	want := "line1\nline2"
	if e.Value() != want {
		t.Errorf("after paste, Value() = %q, want %q", e.Value(), want)
	}
}
