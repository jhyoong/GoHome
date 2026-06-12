package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
)

// handleApprovalReq processes an incoming approval request. If the request is
// for the focused session and no approval is currently active, it becomes the
// active prompt; otherwise it is queued in pendingApprovals.
func (m *Model) handleApprovalReq(msg approvalReqMsg) {
	if msg.Req.SessionID == m.focused && m.activeApproval == nil {
		ap := newApprovalPrompt(msg.Req, msg.Reply)
		m.activeApproval = ap
	} else {
		m.pendingApprovals[msg.Req.SessionID] = newApprovalPrompt(msg.Req, msg.Reply)
	}
}

// handleApprovalKey routes a key press when an approval prompt is active.
// It returns a Cmd (may be nil).
func (m *Model) handleApprovalKey(msg tea.KeyMsg) tea.Cmd {
	ap := m.activeApproval
	var cmds []tea.Cmd

	// --- steer sub-mode ---
	if ap.steering {
		switch msg.Type {
		case tea.KeyEnter:
			steer := strings.TrimSpace(ap.steerInput.Value())
			m.resolveApproval(guard.ApprovalDecision{
				Outcome:      guard.DenySteer,
				SteerMessage: steer,
			})
		case tea.KeyEsc:
			// Cancel steer, return to approval menu.
			ap.steering = false
			ap.steerInput.SetValue("")
			ap.steerInput.Blur()
		default:
			var tiCmd tea.Cmd
			ap.steerInput, tiCmd = ap.steerInput.Update(msg)
			cmds = append(cmds, tiCmd)
		}
		return tea.Batch(cmds...)
	}

	// --- pattern edit sub-mode ---
	if ap.editing {
		switch msg.Type {
		case tea.KeyEnter:
			// Confirm the edited pattern.
			ap.pattern = ap.patternInput.Value()
			ap.editing = false
			ap.patternInput.Blur()
		case tea.KeyEsc:
			// Revert: restore original pattern, exit edit mode.
			ap.patternInput.SetValue(ap.pattern)
			ap.editing = false
			ap.patternInput.Blur()
		default:
			var tiCmd tea.Cmd
			ap.patternInput, tiCmd = ap.patternInput.Update(msg)
			cmds = append(cmds, tiCmd)
		}
		return tea.Batch(cmds...)
	}

	// --- top-level approval menu ---
	switch {
	case msg.Type == tea.KeyUp:
		if ap.selected > 0 {
			ap.selected--
		}
	case msg.Type == tea.KeyDown:
		if ap.selected < 3 {
			ap.selected++
		}
	case msg.Type == tea.KeyEnter:
		switch ap.selected {
		case 0:
			m.resolveApproval(guard.ApprovalDecision{Outcome: guard.AllowOnce})
		case 1:
			m.resolveApproval(guard.ApprovalDecision{
				Outcome:      guard.AllowAlways,
				SavedPattern: ap.pattern,
			})
		case 2:
			m.resolveApproval(guard.ApprovalDecision{Outcome: guard.Deny})
		case 3:
			ap.steering = true
			ap.steerInput.Focus()
		}
	case msg.Type == tea.KeyEsc:
		m.resolveApproval(guard.ApprovalDecision{Outcome: guard.Deny})
	case keyRune(msg) == '1':
		m.resolveApproval(guard.ApprovalDecision{Outcome: guard.AllowOnce})
	case keyRune(msg) == '2':
		m.resolveApproval(guard.ApprovalDecision{
			Outcome:      guard.AllowAlways,
			SavedPattern: ap.pattern,
		})
	case keyRune(msg) == '3':
		m.resolveApproval(guard.ApprovalDecision{Outcome: guard.Deny})
	case keyRune(msg) == '4':
		ap.steering = true
		ap.steerInput.Focus()
	case keyRune(msg) == 'e':
		ap.editing = true
		ap.patternInput.SetValue(ap.pattern)
		ap.patternInput.Focus()
		ap.patternInput.CursorEnd()
	}
	return tea.Batch(cmds...)
}

// resolveApproval sends dec on the active approval's reply channel and clears
// the active approval. If another pending approval exists for the focused
// session, it is promoted to active.
func (m *Model) resolveApproval(dec guard.ApprovalDecision) {
	if m.activeApproval == nil {
		return
	}
	m.activeApproval.reply <- dec
	m.activeApproval = nil
	// Promote any pending approval for the now-focused session.
	m.promoteApproval()
}

// promoteApproval checks whether the focused session has a pending approval
// and, if so, sets it as the active approval.
func (m *Model) promoteApproval() {
	if m.activeApproval != nil {
		return
	}
	if ap, ok := m.pendingApprovals[m.focused]; ok {
		m.activeApproval = ap
		delete(m.pendingApprovals, m.focused)
	}
}

// notificationLine returns a warning string when a non-focused session needs
// approval (or another session is in-flight), or the highest context warning,
// or "" when quiet.
func (m *Model) notificationLine() string {
	// Pending approvals take priority.
	for sid := range m.pendingApprovals {
		if sid != m.focused {
			return fmt.Sprintf("! [%s] needs approval -- Ctrl+] to focus", sid)
		}
	}
	// Secondary: another session is in-flight while we are focused elsewhere.
	for _, id := range m.order {
		if id != m.focused {
			if sv, ok := m.sessions[id]; ok && sv.InFlight {
				return fmt.Sprintf("! [%s] is running", id)
			}
		}
	}
	// Context fullness warning for the focused session (Task 11.16).
	if m.contextNotice != "" {
		return m.contextNotice
	}
	return ""
}
