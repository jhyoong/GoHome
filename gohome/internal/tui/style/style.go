package style

import "github.com/charmbracelet/lipgloss"

// Theme holds lipgloss styles for the TUI.
type Theme struct {
	StatusBar    lipgloss.Style
	Notification lipgloss.Style
}

// Default returns a Theme with sensible terminal-aware defaults.
func Default() Theme {
	return Theme{
		StatusBar: lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")).
			Background(lipgloss.Color("0")),
		Notification: lipgloss.NewStyle().
			Foreground(lipgloss.Color("11")),
	}
}
