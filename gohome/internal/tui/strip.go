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
// If an approval was active for the old session, it goes back to pending.
// If the newly focused session has a pending approval, it is promoted.
func (m *Model) focusNext() {
	if len(m.order) <= 1 {
		return
	}
	m.demoteActiveApproval()
	idx := m.focusedIndex()
	m.focused = m.order[(idx+1)%len(m.order)]
	m.promoteApproval()
	m.rebuildViewport()
}

// focusPrev moves focus to the previous session in order (wraps around).
// If an approval was active for the old session, it goes back to pending.
// If the newly focused session has a pending approval, it is promoted.
func (m *Model) focusPrev() {
	if len(m.order) <= 1 {
		return
	}
	m.demoteActiveApproval()
	idx := m.focusedIndex()
	m.focused = m.order[(idx-1+len(m.order))%len(m.order)]
	m.promoteApproval()
	m.rebuildViewport()
}

// demoteActiveApproval moves the current activeApproval (if any) back into
// pendingApprovals keyed by its session ID, and clears activeApproval.
func (m *Model) demoteActiveApproval() {
	if m.activeApproval == nil {
		return
	}
	// Reset sub-modes so the prompt is presented fresh on next focus.
	ap := m.activeApproval
	ap.editing = false
	ap.steering = false
	ap.patternInput.Blur()
	ap.steerInput.Blur()
	m.pendingApprovals[ap.req.SessionID] = ap
	m.activeApproval = nil
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
