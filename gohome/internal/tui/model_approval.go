package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
)

// handleApprovalReq processes an incoming approval request. If no approval is
// currently active, it becomes the active prompt; otherwise it is appended to
// the FIFO approval queue. It returns a command that re-enables mouse capture
// if the idle timeout had turned it off.
func (m *Model) handleApprovalReq(msg approvalReqMsg) tea.Cmd {
	ap := newApprovalPrompt(msg.Req, msg.Reply)
	if m.activeApproval == nil {
		m.activeApproval = ap
	} else {
		m.approvalQueue = append(m.approvalQueue, ap)
	}
	// Keep wheel events as mouse events so they cannot move the menu.
	// With capture off, many terminals send wheel scrolls as Up/Down keys.
	if m.mouseEnabled && !m.mouseActive {
		m.mouseActive = true
		return func() tea.Msg { return tea.EnableMouseCellMotion() }
	}
	return nil
}

// handleApprovalKey routes a key press when an approval prompt is active.
// It returns a Cmd (may be nil).
func (m *Model) handleApprovalKey(msg tea.KeyMsg) tea.Cmd {
	ap := m.activeApproval
	if ap.sudoStage {
		return m.handleSudoPasswordKey(msg)
	}
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
	if m.scrollApprovalPage(msg) {
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
			cmds = append(cmds, m.allowApproval(guard.AllowOnce))
		case 1:
			cmds = append(cmds, m.allowApproval(guard.AllowAlways))
		case 2:
			cmds = append(cmds, m.resolveApproval(guard.ApprovalDecision{Outcome: guard.Deny}))
		case 3:
			ap.steering = true
			ap.steerInput.Focus()
		}
	case msg.Type == tea.KeyEsc:
		cmds = append(cmds, m.resolveApproval(guard.ApprovalDecision{Outcome: guard.Deny}))
	case keyRune(msg) == '1':
		cmds = append(cmds, m.allowApproval(guard.AllowOnce))
	case keyRune(msg) == '2':
		cmds = append(cmds, m.allowApproval(guard.AllowAlways))
	case keyRune(msg) == '3':
		cmds = append(cmds, m.resolveApproval(guard.ApprovalDecision{Outcome: guard.Deny}))
	case keyRune(msg) == '4':
		ap.steering = true
		ap.steerInput.Focus()
	case keyRune(msg) == 'v' || keyRune(msg) == 'V':
		ap.expandedSummary = !ap.expandedSummary
	case keyRune(msg) == 'e' && !ap.needsSudo:
		ap.editing = true
		ap.patternInput.SetValue(ap.pattern)
		ap.patternInput.Focus()
		ap.patternInput.CursorEnd()
	}
	return tea.Batch(cmds...)
}

// scrollApprovalPage scrolls the timeline on PgUp/PgDown while an approval is
// active. It reports whether the key was handled.
func (m *Model) scrollApprovalPage(msg tea.KeyMsg) bool {
	if msg.Type != tea.KeyPgUp && msg.Type != tea.KeyPgDown {
		return false
	}
	scrollAmt := m.chat.maxHeight / 2
	if scrollAmt < 1 {
		scrollAmt = 1
	}
	m.chat.DisableAutoScroll(m.winW)
	if msg.Type == tea.KeyPgUp {
		m.chat.ScrollUp(scrollAmt)
	} else {
		m.chat.ScrollDown(scrollAmt)
		m.chat.ReEnableAutoScrollIfAtBottom(m.winW)
	}
	return true
}

// allowApproval resolves an Allow decision. Sudo commands first open the
// password stage; the decision is sent when the password is submitted.
func (m *Model) allowApproval(outcome guard.ApprovalOutcome) tea.Cmd {
	ap := m.activeApproval
	if !ap.needsSudo {
		return m.resolveApproval(m.buildApprovalDecision(outcome))
	}
	if m.settings.CacheSudoPassword && m.sudoPasswordCache != "" {
		m.addNotice(ap.req.SessionID, "Using cached sudo password")
		dec := m.buildApprovalDecision(outcome)
		dec.SudoPassword = m.sudoPasswordCache
		return m.resolveApproval(dec)
	}
	ap.sudoStage = true
	ap.sudoOutcome = outcome
	ap.sudoErr = ""
	ap.passwordInput.SetValue("")
	return ap.passwordInput.Focus()
}

// addNotice appends a notice to the session's timeline without forcing the
// view to scroll, so a user who scrolled up keeps their position.
func (m *Model) addNotice(sessionID, text string) {
	sv := m.getOrCreateSession(sessionID, 1)
	sv.Timeline = append(sv.Timeline, TimelineEntry{Kind: KindNotice, Text: text})
	if sessionID == m.focused {
		m.rebuildViewport()
	}
}

// sudoRejected reports whether shell output contains sudo's standard
// wrong-password messages.
func sudoRejected(output string) bool {
	return strings.Contains(output, "Sorry, try again") ||
		strings.Contains(output, "incorrect password attempt")
}

// handleSudoPasswordKey routes keys while the password stage is open. Every
// key except Enter, Esc and PgUp/PgDown goes to the password field.
func (m *Model) handleSudoPasswordKey(msg tea.KeyMsg) tea.Cmd {
	ap := m.activeApproval
	if m.scrollApprovalPage(msg) {
		return nil
	}
	switch msg.Type {
	case tea.KeyEnter:
		if ap.passwordInput.Value() == "" {
			ap.sudoErr = "Password required"
			return nil
		}
		return m.resolveApproval(m.buildApprovalDecision(ap.sudoOutcome))
	case tea.KeyEsc:
		ap.sudoStage = false
		ap.sudoErr = ""
		ap.passwordInput.SetValue("")
		ap.passwordInput.Blur()
		return nil
	}
	ap.sudoErr = ""
	var cmd tea.Cmd
	ap.passwordInput, cmd = ap.passwordInput.Update(msg)
	return cmd
}

// buildApprovalDecision creates an ApprovalDecision for the given outcome,
// attaching sudo password and pattern as appropriate.
func (m *Model) buildApprovalDecision(outcome guard.ApprovalOutcome) guard.ApprovalDecision {
	dec := guard.ApprovalDecision{Outcome: outcome}
	if m.activeApproval != nil && m.activeApproval.needsSudo {
		dec.SudoPassword = m.activeApproval.passwordInput.Value()
	}
	if outcome == guard.AllowAlways && m.activeApproval != nil {
		dec.SavedPattern = m.activeApproval.pattern
	}
	return dec
}

// resolveApproval sends dec on the active approval's reply channel and clears
// the active approval. The next queued approval (if any) is promoted to active.
func (m *Model) resolveApproval(dec guard.ApprovalDecision) tea.Cmd {
	if m.activeApproval == nil {
		return nil
	}
	if m.settings.CacheSudoPassword && m.activeApproval.needsSudo && dec.SudoPassword != "" {
		m.sudoPasswordCache = dec.SudoPassword
	}
	m.activeApproval.reply <- dec
	m.activeApproval = nil
	m.promoteApproval()

	var cmds []tea.Cmd
	// The last approval closed: resume the normal mouse idle timeout.
	if m.activeApproval == nil && m.mouseEnabled && m.mouseActive {
		cmds = append(cmds, m.scheduleMouseIdle())
	}
	if m.activeApproval == nil && (dec.Outcome == guard.AllowOnce || dec.Outcome == guard.AllowAlways) {
		m.spinner.Start("Processing...")
		m.spinner.SetOnCancel(m.cancelFocusedSession)
		cmds = append(cmds, m.spinnerTickCmd())
	}
	return tea.Batch(cmds...)
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
