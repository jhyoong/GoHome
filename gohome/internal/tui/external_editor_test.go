package tui

import (
	"fmt"
	"testing"
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
