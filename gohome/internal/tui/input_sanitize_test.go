package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// rawInput holds an SGR color, an OSC title and a bidi override, as from a
// paste or a file name picked in the file search list.
const rawInput = "a\x1b[31mred\x1b]0;title\x07b\u202ec"

// assertNoRawInput fails if out holds any control sequence other than the
// editor's own reverse-video cursor.
func assertNoRawInput(t *testing.T, out string) {
	t.Helper()
	out = strings.ReplaceAll(out, "\x1b[7m", "")
	out = strings.ReplaceAll(out, "\x1b[0m", "")
	if strings.Contains(out, "\x1b[31m") || strings.Contains(out, "\x1b]") ||
		strings.Contains(out, "\x07") || strings.ContainsRune(out, 0x202e) {
		t.Errorf("render holds raw control characters: %q", out)
	}
}

func TestEditorRenderSanitizesText(t *testing.T) {
	e := NewEditor(80, 40)
	e.InsertText(rawInput)
	assertNoRawInput(t, strings.Join(e.Render(80), "\n"))
	if e.Value() != rawInput {
		t.Errorf("Value = %q, want the raw text %q", e.Value(), rawInput)
	}
}

func TestEditorRenderSanitizesWithCursorInside(t *testing.T) {
	e := NewEditor(80, 40)
	e.InsertText(rawInput)
	for i := 0; i < len([]rune(rawInput)); i++ {
		e.HandleInput(tea.KeyMsg{Type: tea.KeyLeft})
		assertNoRawInput(t, strings.Join(e.Render(80), "\n"))
	}
}

func TestChatUserEntrySanitized(t *testing.T) {
	entries := []TimelineEntry{{Kind: KindUser, Text: rawInput}}
	c := NewChat(&entries, 20)
	out := strings.Join(c.Render(80), "\n")
	if strings.Contains(out, "\x1b[31m") || strings.Contains(out, "\x1b]") || strings.ContainsRune(out, 0x202e) {
		t.Errorf("user entry holds raw control characters: %q", out)
	}
	if !strings.Contains(StripAnsi(out), "aredbc") {
		t.Errorf("user entry text missing: %q", StripAnsi(out))
	}
}

func TestPendingMessagesSanitized(t *testing.T) {
	msgs := []string{rawInput}
	out := strings.Join(NewPendingMessages(&msgs).Render(80), "\n")
	if strings.Contains(out, "\x1b[31m") || strings.Contains(out, "\x1b]") || strings.ContainsRune(out, 0x202e) {
		t.Errorf("queued message holds raw control characters: %q", out)
	}
}
