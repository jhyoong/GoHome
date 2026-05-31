package tui

import (
	"strings"
	"testing"
)

func TestEditorRenderEmpty(t *testing.T) {
	e := NewEditor(80, 24)
	lines := e.Render(80)
	if len(lines) < 5 {
		t.Errorf("expected at least 5 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "─") {
		t.Errorf("first line should be border, got %q", lines[0])
	}
}

func TestEditorRenderWithContent(t *testing.T) {
	e := NewEditor(80, 24)
	e.InsertRune('h')
	e.InsertRune('i')
	lines := e.Render(80)
	joined := strings.Join(lines, "\n")
	plain := StripAnsi(joined)
	if !strings.Contains(plain, "hi") {
		t.Errorf("content 'hi' not found in render: %q", plain)
	}
}

func TestEditorSubmit(t *testing.T) {
	e := NewEditor(80, 24)
	e.InsertRune('t')
	e.InsertRune('e')
	e.InsertRune('s')
	e.InsertRune('t')
	text, ok := e.Submit()
	if !ok {
		t.Fatal("Submit() returned ok=false")
	}
	if text != "test" {
		t.Errorf("Submit() = %q, want %q", text, "test")
	}
	if e.Value() != "" {
		t.Errorf("after submit, Value() = %q, want empty", e.Value())
	}
}

func TestEditorNewline(t *testing.T) {
	e := NewEditor(80, 24)
	e.InsertRune('a')
	e.InsertNewline()
	e.InsertRune('b')
	val := e.Value()
	if val != "a\nb" {
		t.Errorf("Value() = %q, want %q", val, "a\nb")
	}
}

func TestEditorDynamicHeight(t *testing.T) {
	e := NewEditor(80, 30)
	for i := 0; i < 10; i++ {
		if i > 0 {
			e.InsertNewline()
		}
		e.InsertRune('x')
	}
	lines := e.Render(80)
	maxH := 9
	if len(lines) > maxH+2 {
		t.Errorf("editor rendered %d lines, expected <= %d (maxH=%d + 2 borders)", len(lines), maxH+2, maxH)
	}
}

func TestEditorEmptySubmitReturnsNotOk(t *testing.T) {
	e := NewEditor(80, 24)
	_, ok := e.Submit()
	if ok {
		t.Error("Submit() on empty editor should return ok=false")
	}
}
