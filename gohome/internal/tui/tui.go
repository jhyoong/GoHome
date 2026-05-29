package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/tui/style"
)

// TimelineEntry is a single item in a session's conversation history.
type TimelineEntry struct {
	Kind       string // "user" | "assistant" | "tool" | "notice"
	Text       string
	ToolName   string
	ToolResult string
	Expanded   bool
}

// SessionView holds the display state for one agent session.
type SessionView struct {
	ID       string
	Depth    int
	Title    string
	Timeline []TimelineEntry
	InFlight bool
	Usage    common.Usage
}

// Model is the root Bubble Tea model for gohome.
type Model struct {
	theme    style.Theme
	sessions map[string]*SessionView
	order    []string
	focused  string

	input   textarea.Model
	inputCh chan string // shared with Frontend.input
}

// New creates and returns a new Model with an initial "main" session.
// fe may be nil (tests that do not need agent routing or input submission).
// When fe is non-nil, the Model shares fe.input so submitted text reaches
// AwaitUserInput.
func New(fe *Frontend) *Model {
	main := &SessionView{
		ID:    "main",
		Depth: 0,
		Title: "main",
	}

	ta := textarea.New()
	ta.Placeholder = "Type a message..."
	ta.ShowLineNumbers = false
	ta.SetHeight(3)

	var inputCh chan string
	if fe != nil {
		inputCh = fe.input
	} else {
		inputCh = make(chan string, 1)
	}

	m := &Model{
		theme:    style.Default(),
		sessions: map[string]*SessionView{"main": main},
		order:    []string{"main"},
		focused:  "main",
		input:    ta,
		inputCh:  inputCh,
	}
	return m
}

// Init implements tea.Model. Focuses the textarea on startup.
func (m *Model) Init() tea.Cmd {
	return m.input.Focus()
}

// getOrCreateSession returns the SessionView for id, creating it if absent.
func (m *Model) getOrCreateSession(id string, depth int) *SessionView {
	sv, ok := m.sessions[id]
	if !ok {
		sv = &SessionView{ID: id, Title: id, Depth: depth}
		m.sessions[id] = sv
		m.order = append(m.order, id)
	}
	return sv
}

// handleAgentEvent updates the relevant SessionView based on the event kind.
func (m *Model) handleAgentEvent(msg agentEventMsg) {
	ev := msg.Ev
	sv := m.getOrCreateSession(msg.SessionID, 1)

	switch ev.Kind {
	case agent.EventTokenDelta:
		// Append to the last assistant entry if it is in-progress, else add new.
		n := len(sv.Timeline)
		if n > 0 && sv.Timeline[n-1].Kind == "assistant" {
			sv.Timeline[n-1].Text += ev.TextDelta
		} else {
			sv.Timeline = append(sv.Timeline, TimelineEntry{
				Kind: "assistant",
				Text: ev.TextDelta,
			})
		}

	case agent.EventToolCallDone:
		sv.Timeline = append(sv.Timeline, TimelineEntry{
			Kind:     "tool",
			ToolName: ev.ToolName,
			Text:     ev.InputJSON,
		})

	case agent.EventToolResult:
		// Set ToolResult on the most recent tool entry without a result.
		content := ""
		if ev.Result != nil {
			content = ev.Result.Content
		}
		set := false
		for i := len(sv.Timeline) - 1; i >= 0; i-- {
			if sv.Timeline[i].Kind == "tool" && sv.Timeline[i].ToolResult == "" {
				sv.Timeline[i].ToolResult = content
				set = true
				break
			}
		}
		if !set {
			sv.Timeline = append(sv.Timeline, TimelineEntry{
				Kind:       "tool",
				ToolResult: content,
			})
		}

	case agent.EventUsageUpdated:
		if ev.Usage != nil {
			sv.Usage = *ev.Usage
		}

	case agent.EventTurnDone:
		sv.InFlight = false

	case agent.EventSessionStarted:
		// Subagent session — depth 1, add to order if not already present.
		m.getOrCreateSession(ev.SessionID, 1)

	case agent.EventSessionEnded:
		sv.InFlight = false

	case agent.EventError:
		errText := ""
		if ev.Err != nil {
			errText = ev.Err.Error()
		}
		sv.Timeline = append(sv.Timeline, TimelineEntry{
			Kind: "notice",
			Text: errText,
		})
		sv.InFlight = false
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

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.input.SetWidth(msg.Width)

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit

		case tea.KeyEnter:
			// Shift+Enter passes through to textarea for newlines.
			if !msg.Alt {
				text := strings.TrimSpace(m.input.Value())
				if text != "" {
					// Add user entry to focused session.
					sv := m.getOrCreateSession(m.focused, 0)
					sv.Timeline = append(sv.Timeline, TimelineEntry{
						Kind: "user",
						Text: text,
					})
					sv.InFlight = true

					m.input.Reset()
					cmds = append(cmds, m.sendInputCmd(text))
				}
				return m, tea.Batch(cmds...)
			}
		}
		// All other keystrokes go to the textarea.
		var taCmd tea.Cmd
		m.input, taCmd = m.input.Update(msg)
		cmds = append(cmds, taCmd)

	case agentEventMsg:
		m.handleAgentEvent(msg)

	default:
		// Pass through to textarea for cursor blink etc.
		var taCmd tea.Cmd
		m.input, taCmd = m.input.Update(msg)
		cmds = append(cmds, taCmd)
	}

	return m, tea.Batch(cmds...)
}

// AddTimelineEntry appends an entry to the named session's timeline.
// It creates the session if it does not exist. Used in tests and by Update.
func (m *Model) AddTimelineEntry(sessionID string, e TimelineEntry) {
	sv := m.getOrCreateSession(sessionID, 1)
	sv.Timeline = append(sv.Timeline, e)
}

// renderTimeline converts a SessionView's timeline to plain text.
func renderTimeline(sv *SessionView) string {
	var sb strings.Builder
	for _, e := range sv.Timeline {
		switch e.Kind {
		case "user":
			sb.WriteString("> ")
			sb.WriteString(e.Text)
			sb.WriteString("\n")
		case "assistant":
			sb.WriteString(e.Text)
			sb.WriteString("\n")
		case "tool":
			sb.WriteString("tool: ")
			sb.WriteString(e.ToolName)
			sb.WriteString("\n")
		case "notice":
			sb.WriteString("[notice] ")
			sb.WriteString(e.Text)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// View implements tea.Model.
func (m *Model) View() string {
	sv, ok := m.sessions[m.focused]
	if !ok {
		return "gohome\n" + m.input.View()
	}
	content := renderTimeline(sv)
	if content == "" {
		content = "gohome\n"
	}
	return content + m.input.View()
}
