package tui

import tea "github.com/charmbracelet/bubbletea"

// Component is the rendering contract for all TUI elements.
// Render returns terminal lines for the given available width.
// A component that has nothing to show returns an empty slice (zero height).
type Component interface {
	Render(width int) []string
}

// Interactive is a Component that can also receive keyboard input.
type Interactive interface {
	Component
	HandleInput(msg tea.KeyMsg) tea.Cmd
}
