package tui

import "fmt"

// handleExternalEditorResult processes the result of an external editor invocation.
func (m *Model) handleExternalEditorResult(msg ExternalEditorMsg) {
	if msg.Err != nil {
		m.statusMsg = fmt.Sprintf("editor: %v", msg.Err)
	} else {
		m.editor.SetValue(msg.Content)
	}
}
