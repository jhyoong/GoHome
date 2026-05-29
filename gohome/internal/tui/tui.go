package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/tui/style"
)

// Model is the root Bubble Tea model for gohome.
type Model struct {
	theme style.Theme
}

// New creates and returns a new Model.
func New() *Model {
	return &Model{
		theme: style.Default(),
	}
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		}
	}
	return m, nil
}

// View implements tea.Model.
func (m *Model) View() string {
	return "gohome\n"
}
