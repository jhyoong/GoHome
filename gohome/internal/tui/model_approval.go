package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// handleApprovalReq processes an incoming approval request. If no approval is
// currently active, it becomes the active prompt; otherwise it is appended to
// the FIFO approval queue. It returns a command that re-enables mouse capture
// if the idle timeout had turned it off. A password-only request is answered
// at once from the sudo password cache when caching is on and a password is
// stored.
func (m *Model) handleApprovalReq(msg approvalReqMsg) tea.Cmd {
	if m.replyFromCache(msg.Req, msg.Reply) {
		return nil
	}
	ap := newApprovalPrompt(msg.Req, msg.Reply)
	if m.activeApproval == nil {
		m.activeApproval = ap
	} else {
		m.approvalQueue = append(m.approvalQueue, ap)
	}
	if m.spinner.Active() {
		m.spinner.SetMessage("Waiting for approval...")
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
			// Confirm the edited pattern; keep editing if it is invalid.
			if ap.req.Tool == "shell" {
				if _, err := guard.NormalizePattern(ap.patternInput.Value()); err != nil {
					ap.patternErr = err.Error()
					return nil
				}
			}
			ap.patternErr = ""
			ap.pattern = ap.patternInput.Value()
			ap.editing = false
			ap.patternInput.Blur()
		case tea.KeyEsc:
			// Revert: restore original pattern, exit edit mode.
			ap.patternInput.SetValue(ap.pattern)
			ap.patternErr = ""
			ap.editing = false
			ap.patternInput.Blur()
		default:
			ap.patternErr = ""
			var tiCmd tea.Cmd
			ap.patternInput, tiCmd = ap.patternInput.Update(msg)
			cmds = append(cmds, tiCmd)
		}
		return tea.Batch(cmds...)
	}

	// PgUp/PgDown scroll an expanded summary that does not fit, otherwise
	// the timeline.
	if m.scrollApprovalSummary(msg) || m.scrollApprovalPage(msg) {
		return tea.Batch(cmds...)
	}

	// --- top-level approval menu ---
	switch {
	case msg.Type == tea.KeyUp:
		ap.patternErr = ""
		if ap.selected > 0 {
			ap.selected--
		}
	case msg.Type == tea.KeyDown:
		ap.patternErr = ""
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

// scrollApprovalSummary scrolls an expanded summary that is taller than the
// prompt box on PgUp/PgDown. It reports whether the key was handled.
func (m *Model) scrollApprovalSummary(msg tea.KeyMsg) bool {
	ap := m.activeApproval
	if !ap.expandedSummary || ap.scrollMax == 0 {
		return false
	}
	step := max(ap.scrollWin-1, 1)
	switch msg.Type {
	case tea.KeyPgUp:
		ap.scroll = max(ap.scroll-step, 0)
	case tea.KeyPgDown:
		ap.scroll = min(ap.scroll+step, ap.scrollMax)
	default:
		return false
	}
	return true
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
// Allow always on a shell call with an invalid or empty pattern shows the
// error and keeps the prompt open.
func (m *Model) allowApproval(outcome guard.ApprovalOutcome) tea.Cmd {
	ap := m.activeApproval
	if outcome == guard.AllowAlways && ap.req.Tool == "shell" {
		if ap.needsSudo && ap.pattern == "" {
			ap.patternErr = "Allow always is " + allowAlwaysUnavailable
			return nil
		}
		if _, err := guard.NormalizePattern(ap.pattern); err != nil {
			ap.patternErr = err.Error()
			return nil
		}
	}
	ap.patternErr = ""
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
// wrong-password messages, or the pre-auth failure marker the shell tool
// prints, which does not depend on the sudo locale.
func sudoRejected(output string) bool {
	return strings.Contains(output, tools.SudoPreauthFailedMarker) ||
		strings.Contains(output, "Sorry, try again") ||
		strings.Contains(output, "incorrect password attempt")
}

// handleSudoPasswordKey routes keys while the password stage is open. Every
// key except Enter, Esc, Tab and PgUp/PgDown goes to the password field. Tab
// toggles the full command, since 'v' would be typed into the password.
func (m *Model) handleSudoPasswordKey(msg tea.KeyMsg) tea.Cmd {
	ap := m.activeApproval
	if m.scrollApprovalPage(msg) {
		return nil
	}
	switch msg.Type {
	case tea.KeyTab:
		ap.expandedSummary = !ap.expandedSummary
		return nil
	case tea.KeyEnter:
		if ap.passwordInput.Value() == "" {
			ap.sudoErr = "Password required"
			return nil
		}
		return m.resolveApproval(m.buildApprovalDecision(ap.sudoOutcome))
	case tea.KeyEsc:
		// A password-only prompt has no menu to return to.
		if ap.req.PasswordOnly {
			return m.resolveApproval(guard.ApprovalDecision{Outcome: guard.Deny})
		}
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
		m.spinner.SetOnCancel(m.cancelRun)
		cmds = append(cmds, m.spinnerTickCmd())
	}
	return tea.Batch(cmds...)
}

// denyAllApprovals replies Deny to the active approval and every queued one,
// then clears both. Reply channels are buffered, so the sends do not block.
func (m *Model) denyAllApprovals() {
	if m.activeApproval != nil {
		m.activeApproval.reply <- guard.ApprovalDecision{Outcome: guard.Deny}
		m.activeApproval = nil
	}
	for _, ap := range m.approvalQueue {
		ap.reply <- guard.ApprovalDecision{Outcome: guard.Deny}
	}
	m.approvalQueue = nil
}

// replyFromCache answers a password-only request with the cached sudo
// password when caching is on and a password is stored. It reports whether
// the request was answered. Answering ahead of other queued approvals is
// intended: the command is already whitelisted and needs no user input.
// The reply channel is buffered, so the send does not block.
func (m *Model) replyFromCache(req guard.ApprovalRequest, reply chan guard.ApprovalDecision) bool {
	if !req.PasswordOnly || !m.settings.CacheSudoPassword || m.sudoPasswordCache == "" {
		return false
	}
	m.addNotice(req.SessionID, "Using cached sudo password")
	reply <- guard.ApprovalDecision{Outcome: guard.AllowOnce, SudoPassword: m.sudoPasswordCache}
	return true
}

// promoteApproval pops the next approval from the FIFO queue (if any) and
// sets it as the active approval. Password-only prompts that can be answered
// from the cache (filled while they waited) are answered and skipped.
func (m *Model) promoteApproval() {
	for m.activeApproval == nil && len(m.approvalQueue) > 0 {
		next := m.approvalQueue[0]
		m.approvalQueue[0] = nil
		m.approvalQueue = m.approvalQueue[1:]
		if !m.replyFromCache(next.req, next.reply) {
			m.activeApproval = next
		}
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
