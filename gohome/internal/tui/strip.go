package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	chipFocused = lipgloss.NewStyle().Reverse(true).Padding(0, 1)
	chipNormal  = lipgloss.NewStyle().Padding(0, 1)
)

// sessionStrip renders the top single line showing the focused session and
// chips for every session in order.
func (m *Model) sessionStrip() string {
	var sb strings.Builder
	sb.WriteString("Session: ")

	for i, id := range m.order {
		if i > 0 {
			sb.WriteString(" ")
		}
		sv := m.sessions[id]
		state := "done"
		if sv.InFlight {
			state = "running"
		}
		label := id + " " + state
		if id == m.focused {
			sb.WriteString(chipFocused.Render(label))
		} else {
			sb.WriteString(chipNormal.Render(label))
		}
	}

	return sb.String()
}

// focusNext moves focus to the next session in order (wraps around).
func (m *Model) focusNext() {
	if len(m.order) <= 1 {
		return
	}
	idx := m.focusedIndex()
	m.focused = m.order[(idx+1)%len(m.order)]
	m.rebuildViewport()
}

// focusPrev moves focus to the previous session in order (wraps around).
func (m *Model) focusPrev() {
	if len(m.order) <= 1 {
		return
	}
	idx := m.focusedIndex()
	m.focused = m.order[(idx-1+len(m.order))%len(m.order)]
	m.rebuildViewport()
}

// focusedIndex returns the index of m.focused in m.order, or 0 if not found.
func (m *Model) focusedIndex() int {
	for i, id := range m.order {
		if id == m.focused {
			return i
		}
	}
	return 0
}
