package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

// slashCommands is the static list of available slash commands.
var slashCommands = []string{
	"/cancel", "/clear-queue", "/config", "/help", "/model", "/mouse", "/new", "/quit", "/resume", "/tokens", "/yolo",
}

// slashComplete returns all commands in slashCommands that have prefix as a prefix.
func slashComplete(prefix string) []string {
	var out []string
	for _, cmd := range slashCommands {
		if strings.HasPrefix(cmd, prefix) {
			out = append(out, cmd)
		}
	}
	return out
}

// handleSlashCommand parses and executes a slash command string.
// It returns a tea.Cmd when an action requires one (e.g. tea.Quit), or nil.
func (m *Model) handleSlashCommand(raw string) tea.Cmd {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return nil
	}
	cmd := fields[0]
	switch cmd {
	case "/clear-queue":
		n := len(m.pendingMessages)
		m.pendingMessages = m.pendingMessages[:0]
		m.statusMsg = fmt.Sprintf("Cleared %d queued message(s)", n)
	case "/quit":
		return tea.Quit
	case "/yolo":
		m.yolo = !m.yolo
		if m.yolo {
			m.statusMsg = "YOLO mode ON"
		} else {
			m.statusMsg = "YOLO mode OFF"
		}
		if m.onYoloChange != nil {
			m.onYoloChange(m.yolo)
		}
	case "/help":
		helpH := m.winH - stripHeight - statusHeight - 2
		if helpH < 1 {
			helpH = 1
		}
		m.activeModal = NewHelpOverlay(helpH, func() { m.activeModal = nil })
		m.statusMsg = ""
	case "/tokens":
		sv := m.sessions[m.focused]
		if sv != nil {
			m.activeModal = NewTokensOverlay(sv, m.modelName, m.contextWindow, func() { m.activeModal = nil })
			m.statusMsg = ""
		}
	case "/cancel":
		if !m.runActive() {
			m.statusMsg = "Nothing to cancel"
			break
		}
		m.cancelRun()
	case "/new":
		// A swap during a run would move the run's output to the new view.
		if m.runActive() {
			m.statusMsg = "/new: cancel the run first"
			break
		}
		if m.slashCB.NewSession != nil {
			id, err := m.slashCB.NewSession()
			if err != nil {
				m.statusMsg = fmt.Sprintf("/new: %v", err)
			} else {
				m.getOrCreateSession(id, 0)
				m.focused = id
				m.mainID = id
				m.cursor = 0
				m.statusMsg = "New session: " + id
			}
		} else {
			m.statusMsg = "/new: not configured"
		}
	case "/resume":
		if m.runActive() {
			m.statusMsg = "/resume: cancel the run first"
			break
		}
		if m.slashCB.ListSessions == nil {
			m.statusMsg = "/resume: not configured"
			break
		}
		listings, err := m.slashCB.ListSessions()
		if err != nil {
			m.statusMsg = fmt.Sprintf("/resume: %v", err)
			break
		}
		if len(listings) == 0 {
			m.statusMsg = "No sessions found"
			break
		}
		sb := NewSessionBrowser(listings)
		sb.SetOnSelect(func(id string) {
			m.activeModal = nil
			var (
				history  []common.Message
				children []*session.Session
			)
			if m.slashCB.ResumeSession != nil {
				var (
					resolvedID string
					err        error
				)
				resolvedID, history, children, err = m.slashCB.ResumeSession(id)
				if err != nil {
					m.statusMsg = fmt.Sprintf("/resume: %v", err)
					return
				}
				if resolvedID != "" {
					id = resolvedID
				}
			}
			m.LoadResumedSession(id, history, children)
			m.statusMsg = "Resumed: " + id
		})
		sb.SetOnCancel(func() {
			m.activeModal = nil
		})
		sb.SetOnDelete(func(l session.Listing) bool {
			if l.ID == m.mainID || l.ID == m.focused {
				m.statusMsg = "Cannot delete the active session: " + l.ID
				return false
			}
			if err := os.Remove(l.Path); err != nil {
				m.statusMsg = fmt.Sprintf("Failed to delete session %s: %v", l.ID, err)
				return false
			}
			m.statusMsg = "Deleted session: " + l.ID
			return true
		})
		if len(fields) >= 2 {
			sb.SetFilter(fields[1])
		}
		m.activeModal = sb
	case "/model":
		if len(fields) >= 2 {
			name := fields[1]
			if m.slashCB.SetModel != nil {
				model, ctxWin, err := m.slashCB.SetModel(name)
				if err != nil {
					m.statusMsg = fmt.Sprintf("/model: %v", err)
				} else {
					m.modelName = model
					m.configName = name
					if ctxWin > 0 {
						m.contextWindow = ctxWin
					}
					m.settings.DefaultModel = name
					m.statusMsg = "Model set to " + name
				}
			} else {
				m.statusMsg = "/model: not configured"
			}
			break
		}
		if len(m.settings.ModelConfig) == 0 {
			m.statusMsg = fmt.Sprintf("Current model: %s", m.modelName)
			break
		}
		ms := NewModelSelector(m.settings.ModelConfig, m.settings.DefaultModel)
		ms.SetOnSelect(func(configName, _ string) {
			m.activeModal = nil
			if m.slashCB.SetModel == nil {
				m.statusMsg = "/model: not configured"
				return
			}
			model, ctxWin, err := m.slashCB.SetModel(configName)
			if err != nil {
				m.statusMsg = fmt.Sprintf("/model: %v", err)
				return
			}
			m.modelName = model
			m.configName = configName
			if ctxWin > 0 {
				m.contextWindow = ctxWin
			}
			m.settings.DefaultModel = configName
			m.statusMsg = "Model set to " + configName
		})
		ms.SetOnCancel(func() {
			m.activeModal = nil
		})
		m.activeModal = ms
	case "/mouse":
		m.mouseEnabled = !m.mouseEnabled
		if m.mouseEnabled {
			m.mouseActive = true
			m.statusMsg = "Mouse tracking ON (scroll with mouse, auto-disables for text selection)"
			return func() tea.Msg { return tea.EnableMouseCellMotion() }
		}
		m.mouseActive = false
		m.statusMsg = "Mouse tracking OFF (native text selection, no mouse scroll)"
		return func() tea.Msg { return tea.DisableMouse() }
	case "/config":
		if m.slashCB.OpenConfig == nil {
			m.statusMsg = "/config: not configured"
			break
		}
		ann, err := m.slashCB.OpenConfig()
		if err != nil {
			m.statusMsg = fmt.Sprintf("/config: %v", err)
			break
		}
		m.openConfigOverlayWith(ann)
		m.statusMsg = ""
	default:
		m.statusMsg = cmd + ": unknown command"
	}
	return nil
}

// completeSlash fills the editor with the first matching slash command + space.
// Returns true if a completion was applied.
func (m *Model) completeSlash() bool {
	val := m.editor.Value()
	if !strings.HasPrefix(val, "/") {
		return false
	}
	matches := slashComplete(val)
	if len(matches) == 0 {
		return false
	}
	m.editor.SetValue(matches[0] + " ")
	return true
}

var slashHighlight = lipgloss.NewStyle().Bold(true)

// slashPalette renders the autocomplete list when input starts with '/'.
// Returns "" when not applicable.
func (m *Model) slashPalette() string {
	val := m.editor.Value()
	if !strings.HasPrefix(val, "/") {
		return ""
	}
	matches := slashComplete(val)
	if len(matches) == 0 {
		return ""
	}
	parts := make([]string, len(matches))
	parts[0] = slashHighlight.Render(matches[0])
	for i := 1; i < len(matches); i++ {
		parts[i] = matches[i]
	}
	return strings.Join(parts, "  ")
}

// LoadResumedSession shows a resumed session in the focused view and adds a
// view for each of its subagent children, the same way live subagents
// appear. Each child is linked to the parent's subagent tool entry whose task
// equals the child's first user message (newest first, so a repeated task
// links newest child to newest entry). Children with no task match take the
// remaining unlinked entries from the end of the timeline backward, as live
// linking does. Entries with no match stay unlinked; unlinked children still
// get a view. All children are marked completed, so they are read-only.
func (m *Model) LoadResumedSession(id string, history []common.Message, children []*session.Session) {
	sv := m.getOrCreateSession(id, 0)
	sv.Timeline = historyToTimeline(history)

	link := func(i int, childID string) {
		sv.Timeline[i].ChildSessionID = childID
		m.childToParent[childID] = id
	}
	unlinkedSubagent := func(i int) bool {
		e := &sv.Timeline[i]
		return e.Kind == KindTool && e.ToolName == "subagent" && e.ChildSessionID == ""
	}

	for _, child := range children {
		childSV := m.getOrCreateSession(child.ID, 1)
		childSV.Timeline = historyToTimeline(child.History)
		childSV.InFlight = false
		childSV.Completed = true
	}

	var unmatched []*session.Session
	for c := len(children) - 1; c >= 0; c-- {
		child := children[c]
		task := firstUserText(child.History)
		matched := false
		for i := len(sv.Timeline) - 1; task != "" && i >= 0; i-- {
			if unlinkedSubagent(i) && subagentTask(sv.Timeline[i].Text) == task {
				link(i, child.ID)
				matched = true
				break
			}
		}
		if !matched {
			unmatched = append(unmatched, child) // newest first
		}
	}

	next := len(sv.Timeline) - 1
	for _, child := range unmatched {
		for ; next >= 0; next-- {
			if unlinkedSubagent(next) {
				link(next, child.ID)
				next--
				break
			}
		}
	}

	m.focused = id
	m.mainID = id
	m.cursor = len(sv.Timeline) - 1
	m.chat.ScrollToBottom()
	m.rebuildViewport()
}

// firstUserText returns the text of the first user message in history.
func firstUserText(history []common.Message) string {
	for _, msg := range history {
		if msg.Role != common.RoleUser {
			continue
		}
		var parts []string
		for _, b := range msg.Content {
			if b.Kind == common.BlockText {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// subagentTask returns the task field of a subagent call's input JSON, or ""
// if the input is not valid JSON.
func subagentTask(inputJSON string) string {
	var in struct {
		Task string `json:"task"`
	}
	if json.Unmarshal([]byte(inputJSON), &in) != nil {
		return ""
	}
	return in.Task
}

// isSlashCommand reports whether text starts with a known slash command.
// Anything else, such as a pasted path, is sent as a normal message.
func isSlashCommand(text string) bool {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return false
	}
	for _, cmd := range slashCommands {
		if fields[0] == cmd {
			return true
		}
	}
	return false
}
