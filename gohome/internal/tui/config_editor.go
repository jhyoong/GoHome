package tui

import (
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/config"
)

// ConfigEditMsg is sent when the config editor process exits.
type ConfigEditMsg struct {
	Scope string
	Err   error
}

// openConfigEditor ensures the config file at path exists (creating it with the
// skeleton template if needed), then launches the user's preferred editor via
// tea.ExecProcess so Bubble Tea can suspend/resume the TUI.
func openConfigEditor(path, scope string) tea.Cmd {
	if err := config.EnsureConfigFile(path); err != nil {
		return func() tea.Msg {
			return ConfigEditMsg{Scope: scope, Err: err}
		}
	}

	editor := config.ResolveEditor()
	c := exec.Command(editor, path)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return ConfigEditMsg{Scope: scope, Err: err}
	})
}
