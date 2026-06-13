package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHelpOverlay_Render(t *testing.T) {
	overlay := NewHelpOverlay(10, func() {})
	lines := overlay.Render(80)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Keyboard shortcuts") {
		t.Fatal("expected help content in render output")
	}
}

func TestHelpOverlay_EscCloses(t *testing.T) {
	closed := false
	overlay := NewHelpOverlay(20, func() { closed = true })
	overlay.HandleInput(tea.KeyMsg{Type: tea.KeyEsc})
	if !closed {
		t.Fatal("expected Esc to trigger close callback")
	}
}

func TestHelpOverlay_ScrollDown(t *testing.T) {
	overlay := NewHelpOverlay(5, func() {})
	lines1 := overlay.Render(80)
	overlay.HandleInput(tea.KeyMsg{Type: tea.KeyDown})
	overlay.HandleInput(tea.KeyMsg{Type: tea.KeyDown})
	overlay.HandleInput(tea.KeyMsg{Type: tea.KeyDown})
	lines2 := overlay.Render(80)
	if strings.Join(lines1, "\n") == strings.Join(lines2, "\n") {
		t.Fatal("expected render output to change after scrolling")
	}
}

func TestHelpOverlay_ScrollUp(t *testing.T) {
	overlay := NewHelpOverlay(5, func() {})
	overlay.HandleInput(tea.KeyMsg{Type: tea.KeyDown})
	overlay.HandleInput(tea.KeyMsg{Type: tea.KeyDown})
	overlay.HandleInput(tea.KeyMsg{Type: tea.KeyUp})
	overlay.HandleInput(tea.KeyMsg{Type: tea.KeyUp})
	lines := overlay.Render(80)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Keyboard shortcuts") {
		t.Fatal("expected to scroll back to top")
	}
}

func TestHelpOverlay_PgDown(t *testing.T) {
	overlay := NewHelpOverlay(5, func() {})
	overlay.HandleInput(tea.KeyMsg{Type: tea.KeyPgDown})
	lines := overlay.Render(80)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "Keyboard shortcuts") {
		t.Fatal("expected PgDown to scroll past top")
	}
}
