package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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
}

// New creates and returns a new Model with an initial "main" session.
func New() *Model {
	main := &SessionView{
		ID:    "main",
		Depth: 0,
		Title: "main",
	}
	return &Model{
		theme:    style.Default(),
		sessions: map[string]*SessionView{"main": main},
		order:    []string{"main"},
		focused:  "main",
	}
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		}
	}
	return m, nil
}

// AddTimelineEntry appends an entry to the named session's timeline.
// It creates the session if it does not exist. Used in tests and by Update.
func (m *Model) AddTimelineEntry(sessionID string, e TimelineEntry) {
	sv, ok := m.sessions[sessionID]
	if !ok {
		sv = &SessionView{ID: sessionID, Title: sessionID, Depth: 1}
		m.sessions[sessionID] = sv
		m.order = append(m.order, sessionID)
	}
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
		return "gohome\n"
	}
	content := renderTimeline(sv)
	if content == "" {
		return "gohome\n"
	}
	return content
}
