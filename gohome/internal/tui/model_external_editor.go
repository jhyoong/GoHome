package tui

import (
	"fmt"
	"strings"
)

// handleExternalEditorResult processes the result of an external editor invocation.
func (m *Model) handleExternalEditorResult(msg externalEditorMsg) {
	if msg.Err != nil {
		m.statusMsg = fmt.Sprintf("editor: %v", msg.Err)
	} else {
		// Drop \r so CRLF files do not leave stray carriage returns.
		m.editor.SetValue(strings.ReplaceAll(msg.Content, "\r", ""))
	}
}
