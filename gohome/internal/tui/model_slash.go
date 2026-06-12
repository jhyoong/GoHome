package tui

import (
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
	"/help", "/new", "/resume", "/yolo", "/endpoint", "/model", "/cancel", "/tokens", "/quit",
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
		m.showHelp = true
		m.helpScroll = 0
		m.statusMsg = ""
	case "/tokens":
		m.showTokens = true
		m.statusMsg = ""
	case "/cancel":
		m.cancelFocusedSession()
	case "/new":
		if m.slashCB.NewSession != nil {
			id, err := m.slashCB.NewSession()
			if err != nil {
				m.statusMsg = fmt.Sprintf("/new: %v", err)
			} else {
				m.getOrCreateSession(id, 0)
				m.focused = id
				m.cursor = 0
				m.statusMsg = "New session: " + id
			}
		} else {
			m.statusMsg = "/new: not configured"
		}
	case "/resume":
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
			m.browsing = false
			m.sessionBrowser = nil
			var history []common.Message
			if m.slashCB.ResumeSession != nil {
				var err error
				history, err = m.slashCB.ResumeSession(id)
				if err != nil {
					m.statusMsg = fmt.Sprintf("/resume: %v", err)
					return
				}
			}
			sv := m.getOrCreateSession(id, 0)
			sv.Timeline = historyToTimeline(history)
			m.focused = id
			m.cursor = len(sv.Timeline) - 1
			m.statusMsg = "Resumed: " + id
			m.rebuildViewport()
		})
		sb.SetOnCancel(func() {
			m.browsing = false
			m.sessionBrowser = nil
		})
		sb.SetOnDelete(func(l session.Listing) {
			_ = os.Remove(l.Path)
			m.statusMsg = "Deleted session: " + l.ID
		})
		if len(fields) >= 2 {
			sb.SetFilter(fields[1])
		}
		m.sessionBrowser = sb
		m.browsing = true
	case "/model":
		if len(fields) >= 2 {
			name := fields[1]
			if m.slashCB.SetModel != nil {
				err := m.slashCB.SetModel(name)
				if err != nil {
					m.statusMsg = fmt.Sprintf("/model: %v", err)
				} else {
					m.modelName = name
					m.statusMsg = "Model set to " + name
				}
			} else {
				m.statusMsg = "/model: not configured"
			}
			break
		}
		if len(m.settings.Endpoints) == 0 {
			m.statusMsg = fmt.Sprintf("Current model: %s", m.modelName)
			break
		}
		ms := NewModelSelector(m.settings.Endpoints, m.settings.DefaultEndpoint)
		ms.SetOnSelect(func(endpoint, model string) {
			m.selectingModel = false
			m.modelSelector = nil
			if m.slashCB.SetModel != nil {
				if err := m.slashCB.SetModel(model); err != nil {
					m.statusMsg = fmt.Sprintf("/model: %v", err)
					return
				}
			}
			m.modelName = model
			m.settings.DefaultEndpoint = endpoint
			m.statusMsg = "Model set to " + model
		})
		ms.SetOnCancel(func() {
			m.selectingModel = false
			m.modelSelector = nil
		})
		m.modelSelector = ms
		m.selectingModel = true
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
