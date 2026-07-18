package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
)

// handleApprovalReq processes an incoming approval request. If no approval is
// currently active, it becomes the active prompt; otherwise it is appended to
// the FIFO approval queue.
func (m *Model) handleApprovalReq(msg approvalReqMsg) {
	ap := newApprovalPrompt(msg.Req, msg.Reply)
	if m.activeApproval == nil {
		m.activeApproval = ap
	} else {
		m.approvalQueue = append(m.approvalQueue, ap)
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
			cmds = append(cmds, m.resolveApproval(guard.ApprovalDecision{
				Outcome:      guard.DenySteer,
				SteerMessage: steer,
			}))
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

	// PgUp/PgDown scroll the timeline even during approval.
	if msg.Type == tea.KeyPgUp || msg.Type == tea.KeyPgDown {
		scrollAmt := m.chat.maxHeight / 2
		if scrollAmt < 1 {
			scrollAmt = 1
		}
		m.chat.DisableAutoScroll(m.winW)
		if msg.Type == tea.KeyPgUp {
			m.chat.ScrollUp(scrollAmt)
		} else {
			m.chat.ScrollDown(scrollAmt)
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
			cmds = append(cmds, m.resolveApproval(guard.ApprovalDecision{Outcome: guard.AllowOnce}))
		case 1:
			cmds = append(cmds, m.resolveApproval(guard.ApprovalDecision{
				Outcome:      guard.AllowAlways,
				SavedPattern: ap.pattern,
			}))
		case 2:
			cmds = append(cmds, m.resolveApproval(guard.ApprovalDecision{Outcome: guard.Deny}))
		case 3:
			ap.steering = true
			ap.steerInput.Focus()
		}
	case msg.Type == tea.KeyEsc:
		cmds = append(cmds, m.resolveApproval(guard.ApprovalDecision{Outcome: guard.Deny}))
	case keyRune(msg) == '1':
		cmds = append(cmds, m.resolveApproval(guard.ApprovalDecision{Outcome: guard.AllowOnce}))
	case keyRune(msg) == '2':
		cmds = append(cmds, m.resolveApproval(guard.ApprovalDecision{
			Outcome:      guard.AllowAlways,
			SavedPattern: ap.pattern,
		}))
	case keyRune(msg) == '3':
		cmds = append(cmds, m.resolveApproval(guard.ApprovalDecision{Outcome: guard.Deny}))
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
// the active approval. The next queued approval (if any) is promoted to active.
func (m *Model) resolveApproval(dec guard.ApprovalDecision) tea.Cmd {
	if m.activeApproval == nil {
		return nil
	}
	m.activeApproval.reply <- dec
	m.activeApproval = nil
	m.promoteApproval()

	if m.activeApproval == nil && (dec.Outcome == guard.AllowOnce || dec.Outcome == guard.AllowAlways) {
		m.spinner.Start("Processing...")
		m.spinner.SetOnCancel(m.cancelFocusedSession)
		return SpinnerTickCmd()
	}
	return nil
}

// promoteApproval pops the next approval from the FIFO queue (if any) and
// sets it as the active approval.
func (m *Model) promoteApproval() {
	if m.activeApproval != nil {
		return
	}
	if len(m.approvalQueue) > 0 {
		m.activeApproval = m.approvalQueue[0]
		m.approvalQueue[0] = nil
		m.approvalQueue = m.approvalQueue[1:]
	}
}

// notificationLine returns a warning string when approvals are queued,
// another session is in-flight, or a context warning is active, or "" when quiet.
func (m *Model) notificationLine() string {
	if n := len(m.approvalQueue); n > 0 {
		return fmt.Sprintf("! %d more approval(s) queued", n)
	}
	for _, id := range m.order {
		if id != m.focused {
			if sv, ok := m.sessions[id]; ok && sv.InFlight {
				return fmt.Sprintf("! [%s] is running", id)
			}
		}
	}
	if m.contextNotice != "" {
		return m.contextNotice
	}
	return ""
}
