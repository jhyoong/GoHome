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

func TestEditorInsertTextSingleLine(t *testing.T) {
	e := NewEditor(80, 24)
	e.InsertText("hello")
	if e.Value() != "hello" {
		t.Errorf("Value() = %q, want %q", e.Value(), "hello")
	}
}

func TestEditorInsertTextMultiLine(t *testing.T) {
	e := NewEditor(80, 24)
	e.InsertText("line1\nline2\nline3")
	if e.Value() != "line1\nline2\nline3" {
		t.Errorf("Value() = %q, want %q", e.Value(), "line1\nline2\nline3")
	}
}

func TestEditorInsertTextAtCursor(t *testing.T) {
	e := NewEditor(80, 24)
	e.InsertRune('a')
	e.InsertRune('b')
	// cursor is after 'b', col=2
	e.InsertText("X\nY")
	// expected: "abX\nY"
	want := "abX\nY"
	if e.Value() != want {
		t.Errorf("Value() = %q, want %q", e.Value(), want)
	}
}

func TestEditorInsertTextStripsCarriageReturn(t *testing.T) {
	e := NewEditor(80, 24)
	e.InsertText("line1\r\nline2\r\n")
	want := "line1\nline2\n"
	if e.Value() != want {
		t.Errorf("Value() = %q, want %q", e.Value(), want)
	}
}

func TestEditorInsertTextReplacesTab(t *testing.T) {
	e := NewEditor(80, 24)
	e.InsertText("a\tb")
	want := "a    b"
	if e.Value() != want {
		t.Errorf("Value() = %q, want %q", e.Value(), want)
	}
}
