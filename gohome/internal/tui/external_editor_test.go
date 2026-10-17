package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestExternalEditorMsgSetsContent(t *testing.T) {
	m := New(nil, "")
	// Simulate receiving an externalEditorMsg with content.
	msg := ExternalEditorMsg{Content: "edited content", Err: nil}
	m.Update(msg)
	if m.editor.Value() != "edited content" {
		t.Errorf("editor value = %q, want %q", m.editor.Value(), "edited content")
	}
}

func TestExternalEditorMsgWithError(t *testing.T) {
	m := New(nil, "")
	m.editor.InsertRune('x')
	// Simulate an error from the external editor.
	msg := ExternalEditorMsg{Err: fmt.Errorf("editor crashed")}
	m.Update(msg)
	// Editor content should remain unchanged.
	if m.editor.Value() != "x" {
		t.Errorf("editor value = %q, want %q", m.editor.Value(), "x")
	}
	if m.StatusMsg() == "" {
		t.Error("expected status message about error")
	}
}

func TestOpenExternalEditorReturnsCmd(t *testing.T) {
	m := New(nil, "")
	m.editor.InsertText("test content")
	// Set EDITOR to a fast non-interactive command for testing.
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "true")
	cmd := m.openExternalEditor()
	if cmd == nil {
		t.Error("openExternalEditor() returned nil cmd")
	}
}

func TestExternalEditorMsgStripsCR(t *testing.T) {
	m := New(nil, "")
	m.Update(ExternalEditorMsg{Content: "line one\r\nline two\r\n"})
	if got := m.editor.Value(); strings.Contains(got, "\r") {
		t.Errorf("editor value contains \\r: %q", got)
	}
}

func TestCtrlGOpensExternalEditor(t *testing.T) {
	m := New(nil, "")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "true")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	if cmd == nil {
		t.Fatal("Ctrl+G should return the external editor command")
	}
}

func TestCtrlEReachesEditorEndOfLine(t *testing.T) {
	m := New(nil, "")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "true")
	m.editor.InsertText("abc")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	if cmd != nil {
		t.Errorf("Ctrl+E should not open the external editor")
	}
	m.editor.InsertRune('x')
	if got := m.editor.Value(); got != "abcx" {
		t.Errorf("editor value = %q, want %q", got, "abcx")
	}
}
