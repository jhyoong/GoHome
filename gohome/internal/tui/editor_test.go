package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

func TestWrapLineShort(t *testing.T) {
	rows := wrapLine("hello", 0, 20)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].runeLen != 5 {
		t.Errorf("runeLen = %d, want 5", rows[0].runeLen)
	}
}

func TestWrapLineExactWidth(t *testing.T) {
	rows := wrapLine("12345", 0, 5)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
}

func TestWrapLineLongWord(t *testing.T) {
	rows := wrapLine("abcdefghij", 0, 5)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].runeLen != 5 {
		t.Errorf("row 0 runeLen = %d, want 5", rows[0].runeLen)
	}
	if rows[1].startCol != 5 {
		t.Errorf("row 1 startCol = %d, want 5", rows[1].startCol)
	}
}

func TestWrapLineWordBoundary(t *testing.T) {
	rows := wrapLine("hello world foo", 0, 12)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	// "hello world " fits in 12, then "foo" on next row
	if rows[0].runeLen != 12 {
		t.Errorf("row 0 runeLen = %d, want 12", rows[0].runeLen)
	}
}

func TestWrapLineEmpty(t *testing.T) {
	rows := wrapLine("", 0, 20)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].runeLen != 0 {
		t.Errorf("runeLen = %d, want 0", rows[0].runeLen)
	}
}

func TestEditorRenderWrapsLongLine(t *testing.T) {
	e := NewEditor(20, 24)
	for _, r := range "the quick brown fox jumps" {
		e.InsertRune(r)
	}
	lines := e.Render(20)
	// The text should be wrapped across multiple visual lines.
	plain := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(plain, "the quick brown") {
		t.Errorf("expected wrapped content, got:\n%s", plain)
	}
	// Should have more content lines than a single line would produce.
	contentLines := 0
	for _, l := range lines {
		stripped := StripAnsi(l)
		if !strings.Contains(stripped, "─") {
			contentLines++
		}
	}
	if contentLines < 2 {
		t.Errorf("expected at least 2 content lines for wrapped text, got %d", contentLines)
	}
}

func TestEditorVisualRowNavigation(t *testing.T) {
	e := NewEditor(15, 24)
	// Type a line longer than width to trigger wrapping.
	for _, r := range "hello world abcde" {
		e.InsertRune(r)
	}
	// Cursor is at end of the text (logical line 0, col 17).
	if e.cursorLine != 0 {
		t.Fatalf("cursorLine = %d, want 0", e.cursorLine)
	}

	// Press Up to move to the previous visual row (same logical line).
	e.HandleInput(tea.KeyMsg{Type: tea.KeyUp})
	// Should still be on logical line 0 but at an earlier column.
	if e.cursorLine != 0 {
		t.Errorf("after Up: cursorLine = %d, want 0", e.cursorLine)
	}
	if e.cursorCol >= 17 {
		t.Errorf("after Up: cursorCol = %d, should be less than 17", e.cursorCol)
	}

	// Press Down to go back.
	e.HandleInput(tea.KeyMsg{Type: tea.KeyDown})
	if e.cursorLine != 0 {
		t.Errorf("after Down: cursorLine = %d, want 0", e.cursorLine)
	}
}

func TestEditorWrappedScrolling(t *testing.T) {
	e := NewEditor(10, 10)
	// Insert enough text to create many visual rows.
	for i := 0; i < 5; i++ {
		if i > 0 {
			e.InsertNewline()
		}
		for _, r := range "abcdefghijklmno" {
			e.InsertRune(r)
		}
	}
	lines := e.Render(10)
	// Should not panic and should have borders.
	if len(lines) < 2 {
		t.Errorf("expected at least 2 lines (borders), got %d", len(lines))
	}
	first := StripAnsi(lines[0])
	if !strings.Contains(first, "─") {
		t.Errorf("first line should be border, got %q", first)
	}
}
