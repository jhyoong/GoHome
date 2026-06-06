package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSpinnerRenderInactive(t *testing.T) {
	s := NewSpinner()
	lines := s.Render(80)
	if len(lines) != 0 {
		t.Errorf("inactive spinner should render 0 lines, got %d", len(lines))
	}
}

func TestSpinnerRenderActive(t *testing.T) {
	s := NewSpinner()
	s.Start("Thinking...")
	lines := s.Render(80)
	if len(lines) != 1 {
		t.Fatalf("active spinner should render 1 line, got %d", len(lines))
	}
	plain := StripAnsi(lines[0])
	if plain == "" {
		t.Error("spinner line should not be empty")
	}
}

func TestSpinnerTick(t *testing.T) {
	s := NewSpinner()
	s.Start("Working...")
	first := s.Render(80)[0]
	s.Tick()
	second := s.Render(80)[0]
	if first == second {
		t.Error("spinner should change after tick")
	}
}

func TestSpinnerStop(t *testing.T) {
	s := NewSpinner()
	s.Start("Thinking...")
	s.Stop()
	lines := s.Render(80)
	if len(lines) != 0 {
		t.Errorf("stopped spinner should render 0 lines, got %d", len(lines))
	}
}

func TestSpinnerMessageChange(t *testing.T) {
	s := NewSpinner()
	s.Start("Thinking...")
	s.SetMessage("Running bash...")
	lines := s.Render(80)
	plain := StripAnsi(lines[0])
	if plain == "" {
		t.Error("expected non-empty")
	}
	if !strings.Contains(plain, "Running bash...") {
		t.Errorf("expected message 'Running bash...' in %q", plain)
	}
}

func TestSpinnerHandleInputEscCallsCancel(t *testing.T) {
	s := NewSpinner()
	s.Start("Working...")
	called := false
	s.SetOnCancel(func() { called = true })
	s.HandleInput(tea.KeyMsg{Type: tea.KeyEsc})
	if !called {
		t.Error("Escape should call onCancel")
	}
}

func TestSpinnerHandleInputEscNoopWithoutCallback(t *testing.T) {
	s := NewSpinner()
	s.Start("Working...")
	s.HandleInput(tea.KeyMsg{Type: tea.KeyEsc})
}

func TestSpinnerHandleInputIgnoresOtherKeys(t *testing.T) {
	s := NewSpinner()
	s.Start("Working...")
	called := false
	s.SetOnCancel(func() { called = true })
	s.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})
	if called {
		t.Error("non-Escape keys should not trigger onCancel")
	}
}

func TestSpinnerRenderShowsCancelHint(t *testing.T) {
	s := NewSpinner()
	s.Start("Working...")
	s.SetOnCancel(func() {})
	lines := s.Render(80)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	plain := StripAnsi(lines[0])
	if !strings.Contains(plain, "Esc to cancel") {
		t.Errorf("expected cancel hint in %q", plain)
	}
}

func TestSpinnerRenderNoCancelHintWithoutCallback(t *testing.T) {
	s := NewSpinner()
	s.Start("Working...")
	lines := s.Render(80)
	plain := StripAnsi(lines[0])
	if strings.Contains(plain, "Esc to cancel") {
		t.Errorf("should not show cancel hint when onCancel is nil: %q", plain)
	}
}
