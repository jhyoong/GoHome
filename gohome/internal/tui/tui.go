package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
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

// inputHeight is the fixed height reserved for the textarea.
const inputHeight = 3

// statusHeight is the height reserved for the status bar.
const statusHeight = 1

// stripHeight is the height reserved for the session strip at the top.
const stripHeight = 1

// Model is the root Bubble Tea model for gohome.
type Model struct {
	theme    style.Theme
	sessions map[string]*SessionView
	order    []string
	focused  string

	input   textarea.Model
	inputCh chan string // shared with Frontend.input
	vp      viewport.Model
	winW    int
	winH    int
	vpReady bool

	// Phase 12 populates these; Phase 11 renders them.
	modelName     string // LLM model name; "?" when empty
	yolo          bool   // YOLO mode (skip approval)
	contextWindow int    // context window size; defaults to 128000
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
	ta.SetHeight(inputHeight)

	var inputCh chan string
	if fe != nil {
		inputCh = fe.input
	} else {
		inputCh = make(chan string, 1)
	}

	m := &Model{
		theme:         style.Default(),
		sessions:      map[string]*SessionView{"main": main},
		order:         []string{"main"},
		focused:       "main",
		input:         ta,
		inputCh:       inputCh,
		contextWindow: 128000,
	}
	return m
}

// SetModelName sets the LLM model name shown in the status bar.
func (m *Model) SetModelName(name string) {
	m.modelName = name
}

// SetYolo sets YOLO mode. When true the status bar shows a red [YOLO] badge.
func (m *Model) SetYolo(yolo bool) {
	m.yolo = yolo
}

// SetContextWindow sets the total context window size used in the token bar.
// If size <= 0 the default 128000 is used.
func (m *Model) SetContextWindow(size int) {
	if size <= 0 {
		size = 128000
	}
	m.contextWindow = size
}

// Focused returns the ID of the currently focused session.
// Exported for tests that need to inspect focus state.
func (m *Model) Focused() string {
	return m.focused
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

// rebuildViewport refreshes the viewport content from the focused session.
func (m *Model) rebuildViewport() {
	if !m.vpReady {
		return
	}
	sv, ok := m.sessions[m.focused]
	if !ok {
		return
	}
	content := renderTimeline(sv)
	m.vp.SetContent(content)
	// Auto-scroll to bottom when new content arrives.
	m.vp.GotoBottom()
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

	if msg.SessionID == m.focused {
		m.rebuildViewport()
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

// vpHeight returns the viewport height given current window dimensions.
func (m *Model) vpHeight() int {
	h := m.winH - inputHeight - statusHeight - stripHeight
	if h < 1 {
		h = 1
	}
	return h
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.winW = msg.Width
		m.winH = msg.Height
		m.input.SetWidth(msg.Width)
		if !m.vpReady {
			m.vp = viewport.New(msg.Width, m.vpHeight())
			m.vpReady = true
			m.rebuildViewport()
		} else {
			m.vp.Width = msg.Width
			m.vp.Height = m.vpHeight()
		}

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit

		case tea.KeyEnter:
			// Alt+Enter (or Shift+Enter) passes through to textarea for newlines.
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
					m.rebuildViewport()
					cmds = append(cmds, m.sendInputCmd(text))
				}
				return m, tea.Batch(cmds...)
			}
		}

		// PgUp / PgDn and Up/Down route to the viewport.
		// Ctrl+] / Ctrl+[ cycle focus between sessions.
		switch msg.Type {
		case tea.KeyCtrlCloseBracket: // Ctrl+]
			m.focusNext()
		case tea.KeyCtrlOpenBracket: // Ctrl+[
			m.focusPrev()
		case tea.KeyPgUp, tea.KeyPgDown, tea.KeyUp, tea.KeyDown:
			var vpCmd tea.Cmd
			m.vp, vpCmd = m.vp.Update(msg)
			cmds = append(cmds, vpCmd)
		default:
			// All other keystrokes go to the textarea.
			var taCmd tea.Cmd
			m.input, taCmd = m.input.Update(msg)
			cmds = append(cmds, taCmd)
		}

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
	if sessionID == m.focused {
		m.rebuildViewport()
	}
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
	strip := m.sessionStrip()
	sb := m.statusBar()

	if m.vpReady {
		return strip + "\n" + m.vp.View() + "\n" + m.input.View() + "\n" + sb
	}
	// Viewport not yet sized (no WindowSizeMsg received); fall back to plain text.
	sv, ok := m.sessions[m.focused]
	if !ok {
		return strip + "\ngohome\n" + m.input.View() + "\n" + sb
	}
	content := renderTimeline(sv)
	if content == "" {
		content = "gohome\n"
	}
	return strip + "\n" + content + m.input.View() + "\n" + sb
}
