package style

import "github.com/charmbracelet/lipgloss"

// Theme holds lipgloss styles for the TUI.
type Theme struct {
	UserMsg      lipgloss.Style
	AssistantMsg lipgloss.Style
	ToolLine     lipgloss.Style
	StatusBar    lipgloss.Style
	Notification lipgloss.Style
}

// Default returns a Theme with sensible terminal-aware defaults.
func Default() Theme {
	return Theme{
		UserMsg: lipgloss.NewStyle().
			Foreground(lipgloss.Color("12")).
			Bold(true),
		AssistantMsg: lipgloss.NewStyle().
			Foreground(lipgloss.Color("7")),
		ToolLine: lipgloss.NewStyle().
			Foreground(lipgloss.Color("3")).
			Italic(true),
		StatusBar: lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")).
			Background(lipgloss.Color("0")),
		Notification: lipgloss.NewStyle().
			Foreground(lipgloss.Color("11")),
	}
}
