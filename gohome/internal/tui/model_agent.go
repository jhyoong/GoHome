package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
)

// handleAgentEvent updates the relevant SessionView based on the event kind.
// It returns a tea.Cmd (SpinnerTickCmd when the spinner starts, nil otherwise).
func (m *Model) handleAgentEvent(msg agentEventMsg) tea.Cmd {
	ev := msg.Ev
	sv := m.getOrCreateSession(msg.SessionID, 1)
	var dequeuedCmd tea.Cmd

	switch ev.Kind {
	case agent.EventThinkingDelta:
		sv.InFlight = true
		n := len(sv.Timeline)
		if n > 0 && sv.Timeline[n-1].Kind == KindThinking {
			sv.Timeline[n-1].Text += ev.ThinkingDelta
		} else {
			sv.Timeline = append(sv.Timeline, TimelineEntry{
				Kind:     KindThinking,
				Text:     ev.ThinkingDelta,
				Expanded: true,
			})
		}

	case agent.EventThinkingDone:
		n := len(sv.Timeline)
		for i := n - 1; i >= 0; i-- {
			if sv.Timeline[i].Kind == KindThinking {
				sv.Timeline[i].Expanded = false
				break
			}
		}

	case agent.EventTokenDelta:
		// Append to the last assistant entry if it is in-progress, else add new.
		sv.InFlight = true
		n := len(sv.Timeline)
		if n > 0 && sv.Timeline[n-1].Kind == KindAssistant {
			sv.Timeline[n-1].Text += ev.TextDelta
		} else {
			sv.Timeline = append(sv.Timeline, TimelineEntry{
				Kind: KindAssistant,
				Text: ev.TextDelta,
			})
		}

	case agent.EventToolCallDone:
		entry := TimelineEntry{
			Kind:     KindTool,
			ToolName: ev.ToolName,
			Text:     ev.InputJSON,
			Status:   "pending",
		}
		if ev.ToolName == "edit" {
			entry.DiffPreview = buildDiffPreview(ev.InputJSON)
		}
		sv.Timeline = append(sv.Timeline, entry)
		if sv.Depth > 0 {
			shadow := TimelineEntry{
				Kind:        KindTool,
				ToolName:    ev.ToolName,
				Text:        ev.InputJSON,
				Status:      "pending",
				DiffPreview: entry.DiffPreview,
			}
			m.insertShadowEntry(msg.SessionID, shadow)
		}

	case agent.EventToolResult:
		// Set ToolResult on the most recent tool entry without a result.
		content := ""
		isErr := false
		if ev.Result != nil {
			content = ev.Result.Content
			isErr = ev.Result.IsError
		}
		set := false
		for i := len(sv.Timeline) - 1; i >= 0; i-- {
			if sv.Timeline[i].Kind == KindTool && sv.Timeline[i].ToolResult == "" {
				sv.Timeline[i].ToolResult = content
				if isErr {
					sv.Timeline[i].Status = "error"
				} else {
					sv.Timeline[i].Status = "success"
				}
				set = true
				break
			}
		}
		if !set {
			status := "success"
			if isErr {
				status = "error"
			}
			sv.Timeline = append(sv.Timeline, TimelineEntry{
				Kind:       KindTool,
				ToolResult: content,
				Status:     status,
			})
		}
		if sv.Depth > 0 {
			m.updateShadowResult(msg.SessionID, content, isErr)
		}

	case agent.EventUsageUpdated:
		if ev.Usage != nil {
			sv.Usage = *ev.Usage
			m.checkContextWarnings(sv)
		}

	case agent.EventTurnDone:
		sv.InFlight = false
		if msg.SessionID == m.focused && len(m.pendingMessages) > 0 {
			text := m.pendingMessages[0]
			m.pendingMessages = m.pendingMessages[1:]
			sv.Timeline = append(sv.Timeline, TimelineEntry{
				Kind: KindUser,
				Text: text,
			})
			sv.InFlight = true
			m.cursor = len(sv.Timeline) - 1
			dequeuedCmd = m.sendInputCmd(text)
		}

	case agent.EventSessionStarted:
		// Subagent session -- depth 1, add to order if not already present.
		m.getOrCreateSession(ev.SessionID, 1)
		// Link child to parent: find the parent session that has a pending
		// subagent tool entry without a ChildSessionID.
		for _, parentID := range m.order {
			ps := m.sessions[parentID]
			if ps == nil {
				continue
			}
			for i := len(ps.Timeline) - 1; i >= 0; i-- {
				e := &ps.Timeline[i]
				if e.Kind == KindTool && e.ToolName == "subagent" && e.ChildSessionID == "" {
					e.ChildSessionID = ev.SessionID
					m.childToParent[ev.SessionID] = parentID
					break
				}
			}
			if _, ok := m.childToParent[ev.SessionID]; ok {
				break
			}
		}

	case agent.EventSessionEnded:
		sv.InFlight = false
		if ev.EndReason == "done" {
			sv.Completed = true
		}

	case agent.EventSessionSwapped:
		m.focused = ev.SessionID
		m.getOrCreateSession(ev.SessionID, 0)
		m.statusMsg = "Switched to session: " + ev.SessionID

	case agent.EventError:
		errText := ""
		if ev.Err != nil {
			errText = ev.Err.Error()
		}
		sv.Timeline = append(sv.Timeline, TimelineEntry{
			Kind: KindNotice,
			Text: errText,
		})
		sv.InFlight = false
	}

	// Spinner: start on thinking/token delta, stop on completion/error.
	switch ev.Kind {
	case agent.EventThinkingDelta:
		if !m.spinner.Active() {
			m.spinner.Start("Thinking...")
			m.spinner.SetOnCancel(m.cancelFocusedSession)
		}
	case agent.EventTokenDelta:
		if !m.spinner.Active() {
			m.spinner.Start("Generating...")
			m.spinner.SetOnCancel(m.cancelFocusedSession)
		} else {
			m.spinner.SetMessage("Generating...")
		}
	case agent.EventTurnDone, agent.EventSessionEnded, agent.EventError:
		if !sv.InFlight {
			m.spinner.Stop()
		}
	}

	if msg.SessionID == m.focused {
		if m.renderThrottleMs > 0 &&
			(ev.Kind == agent.EventTokenDelta || ev.Kind == agent.EventThinkingDelta) {
			elapsed := time.Since(m.lastRenderTime)
			threshold := time.Duration(m.renderThrottleMs) * time.Millisecond
			if elapsed < threshold {
				if !m.renderPending {
					m.renderPending = true
					remaining := threshold - elapsed
					cmd := tea.Tick(remaining, func(time.Time) tea.Msg {
						return renderThrottleMsg{}
					})
					if dequeuedCmd != nil {
						return tea.Batch(dequeuedCmd, cmd)
					}
					return cmd
				}
				if dequeuedCmd != nil {
					return dequeuedCmd
				}
				if m.spinner.Active() {
					return SpinnerTickCmd()
				}
				return nil
			}
			m.lastRenderTime = time.Now()
		}
		m.rebuildViewport()
	}

	if dequeuedCmd != nil {
		return dequeuedCmd
	}
	if (ev.Kind == agent.EventTokenDelta || ev.Kind == agent.EventThinkingDelta) && m.spinner.Active() {
		return SpinnerTickCmd()
	}
	return nil
}

// insertShadowEntry inserts a shadow tool entry into the parent session's
// timeline after the subagent tool entry linked to childID. Maintains a
// sliding window of maxShadow entries, removing the oldest if needed.
func (m *Model) insertShadowEntry(childID string, entry TimelineEntry) {
	parentID, ok := m.childToParent[childID]
	if !ok {
		return
	}
	ps, ok := m.sessions[parentID]
	if !ok {
		return
	}

	anchorIdx := -1
	for i := range ps.Timeline {
		if ps.Timeline[i].Kind == KindTool && ps.Timeline[i].ChildSessionID == childID {
			anchorIdx = i
			break
		}
	}
	if anchorIdx < 0 {
		return
	}

	const maxShadow = 3
	shadowStart := anchorIdx + 1
	shadowCount := 0
	for j := shadowStart; j < len(ps.Timeline); j++ {
		if ps.Timeline[j].Shadow && ps.Timeline[j].ChildSessionID == childID {
			shadowCount++
		} else {
			break
		}
	}

	if shadowCount >= maxShadow {
		removeIdx := shadowStart
		ps.Timeline = append(ps.Timeline[:removeIdx], ps.Timeline[removeIdx+1:]...)
		if parentID == m.focused && m.cursor > removeIdx {
			m.cursor--
		}
		shadowCount--
	}

	insertIdx := shadowStart + shadowCount
	entry.Shadow = true
	entry.ChildSessionID = childID
	ps.Timeline = append(ps.Timeline[:insertIdx], append([]TimelineEntry{entry}, ps.Timeline[insertIdx:]...)...)
	if parentID == m.focused && m.cursor >= insertIdx {
		m.cursor++
	}
}

// updateShadowResult updates the most recent pending shadow entry for childID
// in the parent's timeline with the tool result.
func (m *Model) updateShadowResult(childID string, content string, isError bool) {
	parentID, ok := m.childToParent[childID]
	if !ok {
		return
	}
	ps, ok := m.sessions[parentID]
	if !ok {
		return
	}
	for i := len(ps.Timeline) - 1; i >= 0; i-- {
		e := &ps.Timeline[i]
		if e.Shadow && e.ChildSessionID == childID && e.ToolResult == "" {
			e.ToolResult = content
			if isError {
				e.Status = "error"
			} else {
				e.Status = "success"
			}
			return
		}
	}
}

// sendInputCmd returns a Cmd that delivers text to the input channel
// without blocking the update loop.
func (m *Model) sendInputCmd(text string) tea.Cmd {
	ch := m.inputCh
	return func() tea.Msg {
		ch <- text
		return nil
	}
}

// checkContextWarnings fires one-time context-fullness warnings for sv.
// It updates sv.warned80/warned95 and m.contextNotice.
func (m *Model) checkContextWarnings(sv *SessionView) {
	if m.contextWindow <= 0 {
		return
	}
	used := sv.Usage.InputTokens + sv.Usage.OutputTokens
	ratio := float64(used) / float64(m.contextWindow)
	if ratio >= m.contextCritPct && !sv.warned95 {
		sv.warned95 = true
		m.contextNotice = "Context near limit -- next turn may fail or truncate."
	} else if ratio >= m.contextWarnPct && !sv.warned80 {
		sv.warned80 = true
		m.contextNotice = "Context 80% full -- consider /new or /resume into a fresh session."
	}
}
