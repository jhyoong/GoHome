package tui

import (
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
		sv.Timeline = append(sv.Timeline, TimelineEntry{
			Kind:     KindTool,
			ToolName: ev.ToolName,
			Text:     ev.InputJSON,
			Status:   "pending",
		})

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

	case agent.EventSessionEnded:
		sv.InFlight = false

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
