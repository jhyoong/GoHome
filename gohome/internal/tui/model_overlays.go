package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// renderTokensOverlay renders the /tokens usage overlay for the focused session.
func (m *Model) renderTokensOverlay() string {
	sv, ok := m.sessions[m.focused]
	if !ok {
		return ""
	}
	u := sv.Usage
	used := u.InputTokens + u.OutputTokens
	total := m.contextWindow
	pct := 0
	if total > 0 {
		pct = int(float64(used) / float64(total) * 100)
		if pct > 100 {
			pct = 100
		}
	}
	modelName := m.modelName
	if modelName == "" {
		modelName = "?"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Token usage -- %s -- %s\n", sv.ID, modelName)
	fmt.Fprintf(&sb, "  Input tokens    %d\n", u.InputTokens)
	fmt.Fprintf(&sb, "  Output tokens   %d\n", u.OutputTokens)
	fmt.Fprintf(&sb, "  Cache reads     %d\n", u.CacheReadTokens)
	fmt.Fprintf(&sb, "  Cache writes    %d\n", u.CacheWriteTokens)
	sb.WriteString("  --------------------\n")
	fmt.Fprintf(&sb, "  Total           %d / %d (%d%%)\n", used, total, pct)
	sb.WriteString("  Esc to close")
	return sb.String()
}

// handleTokensKey handles key input when the /tokens overlay is open.
func (m *Model) handleTokensKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc {
		m.showTokens = false
	}
	return m, nil
}

// handleHelpKey handles key input when the help overlay is open.
func (m *Model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.showHelp = false
		m.helpScroll = 0
	case tea.KeyUp, tea.KeyLeft:
		if m.helpScroll > 0 {
			m.helpScroll--
		}
	case tea.KeyDown, tea.KeyRight:
		m.helpScroll++
	case tea.KeyPgUp:
		m.helpScroll -= 5
		if m.helpScroll < 0 {
			m.helpScroll = 0
		}
	case tea.KeyPgDown:
		m.helpScroll += 5
	}
	return m, nil
}

// handleExternalEditorResult processes the result of an external editor invocation.
func (m *Model) handleExternalEditorResult(msg externalEditorMsg) {
	if msg.Err != nil {
		m.statusMsg = fmt.Sprintf("editor: %v", msg.Err)
	} else {
		m.editor.SetValue(msg.Content)
	}
}
