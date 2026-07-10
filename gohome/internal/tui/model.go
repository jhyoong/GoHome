package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/tui/style"
)

const (
	KindUser      = "user"
	KindAssistant = "assistant"
	KindThinking  = "thinking"
	KindTool      = "tool"
	KindNotice    = "notice"
	KindStats     = "stats"
)

// TurnStatsData holds per-turn performance metrics for display.
type TurnStatsData struct {
	TPS              float64
	OutputTokens     int
	InputTokens      int
	CacheReadTokens  int
	CacheWriteTokens int
	Elapsed          time.Duration
}

// TimelineEntry is a single item in a session's conversation history.
type TimelineEntry struct {
	Kind        string // KindUser | KindAssistant | KindTool | KindNotice | KindStats
	Text        string
	ToolName    string
	ToolResult  string
	Expanded    bool
	Status      string // "" | "pending" | "success" | "error" (tool entries only)
	DiffPreview string // pre-formatted unified diff for edit tool calls

	Duration  time.Duration  // tool execution wall-clock time
	TurnStats *TurnStatsData // per-turn stats (KindStats entries only)

	Shadow         bool   // true for shadow copies of child tool calls
	ChildSessionID string // links subagent tool entry to child session

	cachedLines      []string
	cachedWidth      int
	cachedExpanded   bool
	cachedText       string
	cachedResult     string
	cachedDiffStatus string
}

// cacheValid reports whether the cached render output is still usable
// at the given terminal width.
func (e *TimelineEntry) cacheValid(width int) bool {
	return e.cachedLines != nil &&
		e.cachedWidth == width &&
		e.cachedExpanded == e.Expanded &&
		e.cachedText == e.Text &&
		e.cachedResult == e.ToolResult &&
		e.cachedDiffStatus == e.Status
}

// SessionView holds the display state for one agent session.
type SessionView struct {
	ID        string
	Depth     int
	Title     string
	Timeline  []TimelineEntry
	InFlight  bool
	Completed bool
	Usage     common.Usage

	// Context-fullness warning sentinels (Task 11.16).
	warned80 bool
	warned95 bool
}

// statusHeight is the height reserved for the status bar.
const statusHeight = 1

// stripHeight is the height reserved for the session strip at the top.
const stripHeight = 1

// Model is the root Bubble Tea model for gohome.
type Model struct {
	theme         style.Theme
	sessions      map[string]*SessionView
	order         []string
	focused       string
	childToParent map[string]string

	editor  *EditorComponent
	chat    *ChatComponent
	spinner *SpinnerComponent
	inputCh chan string
	winW    int
	winH    int

	fileSearch    *FileSearchPopup
	fileSearching bool
	activeModal   Interactive

	homeDir string
	cwd     string

	settings config.Settings

	pendingMessages []string
	pending         *PendingMessagesComponent

	// cursor indexes the focused session's Timeline. When input is empty,
	// Up/Down move the cursor; Enter on a tool entry toggles expansion.
	cursor int

	// LLM and context window settings.
	modelName      string  // LLM model name; "?" when empty
	configName     string  // model config key name for status bar
	yolo           bool    // YOLO mode (skip approval)
	contextWindow  int     // context window size; defaults to 128000
	contextWarnPct float64 // ratio at which to show 80% warning
	contextCritPct float64 // ratio at which to show 95% critical warning

	// Git context for the status bar.
	gitBranch  string
	projectDir string

	// Config overlay state.
	configGlobalPath  string
	configProjectPath string
	configEditScope   string

	// Approval overlay state.
	// activeApproval is the prompt currently displayed in the input region.
	// approvalQueue holds pending approvals in FIFO order (any session).
	activeApproval *approvalPrompt
	approvalQueue  []*approvalPrompt

	// statusMsg is a transient message shown near the status bar (Task 11.14).
	statusMsg string

	// onYoloChange is called whenever the /yolo command toggles the YOLO flag.
	// It is set via SetYoloCallback and allows the TUI to propagate the change
	// to the guard without importing the guard package directly.
	onYoloChange func(bool)

	// Context warning tracking per session (Task 11.16).
	// warned80/warned95 are set in handleAgentEvent to fire once per session.
	contextNotice string // most recent context warning for the notification line

	// lastCtrlC records the time of the most recent Ctrl+C press.
	// A second press within 500ms quits; a single press cancels the current turn.
	lastCtrlC time.Time

	// slashCB holds optional callbacks wired to slash commands (/new, /resume,
	// /model, /cancel). Set via SetSlashCallbacks.
	slashCB SlashCallbacks

	renderThrottleMs int
	lastRenderTime   time.Time
	renderPending    bool
}

// renderThrottleMsg fires when a deferred render is due.
type renderThrottleMsg struct{}

// New creates and returns a new Model with an initial session whose ID matches
// the agent session. fe may be nil (tests that do not need agent routing or
// input submission). When fe is non-nil, the Model shares fe.input so submitted
// text reaches AwaitUserInput.
func New(fe *Frontend, sessionID string) *Model {
	if sessionID == "" {
		sessionID = "main"
	}
	main := &SessionView{
		ID:    sessionID,
		Depth: 0,
		Title: "main",
	}

	var inputCh chan string
	if fe != nil {
		inputCh = fe.input
	} else {
		inputCh = make(chan string, 1)
	}

	m := &Model{
		theme:            style.Default(),
		sessions:         map[string]*SessionView{sessionID: main},
		order:            []string{sessionID},
		focused:          sessionID,
		childToParent:    make(map[string]string),
		inputCh:          inputCh,
		contextWindow:    config.DefaultContextWindow,
		contextWarnPct:   config.DefaultContextWarnPct,
		contextCritPct:   config.DefaultContextCritPct,
		editor:           NewEditor(80, 24),
		spinner:          NewSpinner(),
		fileSearch:       NewFileSearchPopup(),
	}
	m.chat = NewChat(&main.Timeline, 20)
	m.pending = NewPendingMessages(&m.pendingMessages)
	return m
}

// SetModelName sets the LLM model name shown in the status bar.
func (m *Model) SetModelName(name string) {
	m.modelName = name
}

// SetConfigName sets the model config key name shown in the status bar.
func (m *Model) SetConfigName(name string) {
	m.configName = name
}

// SetGitBranch sets the git branch displayed in the status bar.
func (m *Model) SetGitBranch(branch string) {
	m.gitBranch = branch
}

// SetProjectDir sets the project directory displayed in the status bar.
func (m *Model) SetProjectDir(dir string) {
	m.projectDir = dir
}

// SetYolo sets YOLO mode. When true the status bar shows a red [YOLO] badge.
func (m *Model) SetYolo(yolo bool) {
	m.yolo = yolo
}

// SetYoloCallback registers a function that is called whenever the /yolo
// command toggles YOLO mode. The argument is the new yolo value.
// This keeps the TUI decoupled from the concrete guard type.
func (m *Model) SetYoloCallback(fn func(bool)) {
	m.onYoloChange = fn
}

// SetSlashCallbacks registers the callbacks invoked by /new, /resume, /cancel,
// and /model slash commands.
func (m *Model) SetSlashCallbacks(cb SlashCallbacks) {
	m.slashCB = cb
}

func (m *Model) SetHomeDir(dir string)         { m.homeDir = dir }
func (m *Model) SetCWD(dir string)             { m.cwd = dir }
func (m *Model) SetSettings(s config.Settings) { m.settings = s }
func (m *Model) SetRenderThrottleMs(ms int)    { m.renderThrottleMs = ms }

// ShowStartupWizard opens the config wizard as a modal overlay. Called from
// main.go when no model configs exist and the user needs guided setup.
func (m *Model) ShowStartupWizard(onSave func(path string)) {
	m.activeModal = NewConfigWizard(
		func() { m.activeModal = nil },
		func(path string) {
			m.activeModal = nil
			if onSave != nil {
				onSave(path)
			}
		},
	)
}

// SetContextWindow sets the total context window size used in the token bar.
// If size <= 0 the default is used.
func (m *Model) SetContextWindow(size int) {
	if size <= 0 {
		size = config.DefaultContextWindow
	}
	m.contextWindow = size
}

// SetContextThresholds sets the warn and critical context-fullness ratios.
func (m *Model) SetContextThresholds(warn, crit float64) {
	if warn <= 0 || crit <= 0 || warn >= crit || warn > 1 || crit > 1 {
		m.contextWarnPct = config.DefaultContextWarnPct
		m.contextCritPct = config.DefaultContextCritPct
		return
	}
	m.contextWarnPct = warn
	m.contextCritPct = crit
}

// StatusBarForTest returns the rendered status bar string. Exported for tests.
func (m *Model) StatusBarForTest() string { return m.statusBar() }

// Focused returns the ID of the currently focused session.
// Exported for tests that need to inspect focus state.
func (m *Model) Focused() string {
	return m.focused
}

// Sessions returns the session views map. Exported for tests.
func (m *Model) Sessions() map[string]*SessionView {
	return m.sessions
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return nil
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

// syncChatHeight recomputes the chat area height from the current window
// dimensions and pushes it to the chat component. This ensures methods like
// EnsureCursorVisible use the correct viewport size even before View() runs.
func (m *Model) syncChatHeight() {
	chatH := m.winH - editorMinHeight - 2 - stripHeight - statusHeight - 2
	if chatH < 1 {
		chatH = 1
	}
	m.chat.SetMaxHeight(chatH)
}

// rebuildViewport refreshes the chat component state from the focused session.
// When keepScroll is false (or omitted), the viewport scrolls to the bottom.
func (m *Model) rebuildViewport(keepScroll ...bool) {
	sv, ok := m.sessions[m.focused]
	if !ok {
		return
	}
	cur := -1
	if strings.TrimSpace(m.editor.Value()) == "" {
		m.clampCursor()
		cur = m.cursor
	}
	m.chat.SetTimeline(&sv.Timeline)
	m.chat.SetCursor(cur)
	if len(keepScroll) == 0 || !keepScroll[0] {
		m.chat.ScrollToBottom()
	}
}

func (m *Model) cancelFocusedSession() {
	m.cancelFocusedSessionWith("Cancelled")
}

func (m *Model) cancelFocusedSessionWith(statusMsg string) {
	if m.slashCB.CancelSession != nil {
		m.slashCB.CancelSession(m.focused)
	}
	sv := m.sessions[m.focused]
	if sv != nil {
		sv.InFlight = false
		sv.Timeline = append(sv.Timeline, TimelineEntry{Kind: KindNotice, Text: "Cancelled."})
	}
	m.pendingMessages = m.pendingMessages[:0]
	m.spinner.Stop()
	m.statusMsg = statusMsg
	m.rebuildViewport()
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.winW = msg.Width
		m.winH = msg.Height
		m.editor.SetTermHeight(msg.Height)

	case spinnerTickMsg:
		if m.spinner.Active() {
			m.spinner.Tick()
			return m, SpinnerTickCmd()
		}

	case approvalReqMsg:
		m.handleApprovalReq(msg)

	case tea.KeyMsg:
		mdl, cmd := m.handleKeyMsg(msg)
		if m.configEditScope != "" {
			scope := m.configEditScope
			m.configEditScope = ""
			switch scope {
			case "global":
				return mdl, openConfigEditor(m.configGlobalPath, scope)
			case "project":
				return mdl, openConfigEditor(m.configProjectPath, scope)
			case "wizard":
				m.activeModal = NewConfigWizard(
					func() { m.activeModal = nil },
					func(path string) {
						m.activeModal = nil
						m.statusMsg = "Config saved. Restart gohome to apply changes."
					},
				)
			}
			return mdl, cmd
		}
		return mdl, cmd

	case FileSearchResultMsg:
		if m.fileSearching {
			m.fileSearch.SetResults(msg.Query, msg.Results)
		}

	case renderThrottleMsg:
		if m.renderPending {
			m.renderPending = false
			m.lastRenderTime = time.Now()
			m.rebuildViewport()
		}

	case agentEventMsg:
		if cmd := m.handleAgentEvent(msg); cmd != nil {
			return m, cmd
		}

	case externalEditorMsg:
		m.handleExternalEditorResult(msg)

	case ConfigEditMsg:
		if msg.Err != nil {
			m.statusMsg = fmt.Sprintf("editor: %v", msg.Err)
		} else {
			m.statusMsg = "Config saved. Restart gohome to apply changes."
		}
	}

	return m, nil
}

// timelineEntryText returns the plain-text content of a TimelineEntry for
// clipboard purposes.
func timelineEntryText(e TimelineEntry) string {
	switch e.Kind {
	case KindUser, KindAssistant, KindThinking, KindNotice:
		return e.Text
	case KindTool:
		var sb strings.Builder
		fmt.Fprintf(&sb, "[tool] %s", e.ToolName)
		if e.Text != "" {
			fmt.Fprintf(&sb, "\nargs: %s", e.Text)
		}
		if e.ToolResult != "" {
			fmt.Fprintf(&sb, "\nresult: %s", e.ToolResult)
		}
		return sb.String()
	default:
		return e.Text
	}
}

// keyRune returns the single rune for a KeyRunes message, or 0 if the message
// carries zero or more than one rune.
func keyRune(msg tea.KeyMsg) rune {
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
		return msg.Runes[0]
	}
	return 0
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

// shortSummary produces a short single-line summary of a (possibly multi-line)
// string. If the string is a single line (no newlines), it is returned as-is
// (truncated to 60 chars). If multi-line, returns "N lines".
func shortSummary(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) == 1 {
		if VisualWidth(s) > 60 {
			return TruncateText(s, 57) + "..."
		}
		return s
	}
	return fmt.Sprintf("%d lines", len(lines))
}

// clampCursor ensures m.cursor is within the valid range for the focused session.
func (m *Model) clampCursor() {
	sv, ok := m.sessions[m.focused]
	if !ok || len(sv.Timeline) == 0 {
		m.cursor = 0
		return
	}
	n := len(sv.Timeline)
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= n {
		m.cursor = n - 1
	}
}

// View implements tea.Model.
func (m *Model) View() string {
	// Guard against zero-size window (no WindowSizeMsg received yet).
	if m.winW <= 0 {
		return "gohome"
	}

	var sections []string

	sections = append(sections, m.sessionStrip())

	if nl := m.notificationLine(); nl != "" {
		sections = append(sections, m.theme.Notification.Render(nl))
	}

	if to, ok := m.activeModal.(*TokensOverlay); ok {
		sections = append(sections, strings.Join(to.Render(m.winW), "\n"))
		sections = append(sections, m.statusBar())
		return strings.Join(sections, "\n")
	}

	if ho, ok := m.activeModal.(*HelpOverlay); ok {
		helpH := m.winH - stripHeight - statusHeight - 2
		if helpH < 1 {
			helpH = 1
		}
		ho.SetMaxH(helpH)
		sections = append(sections, strings.Join(ho.Render(m.winW), "\n"))
		sections = append(sections, m.statusBar())
		return strings.Join(sections, "\n")
	}

	// Chat area
	chatH := m.winH - editorMinHeight - 2 - stripHeight - statusHeight - 2
	if chatH < 1 {
		chatH = 1
	}
	m.chat.SetMaxHeight(chatH)
	sv, ok := m.sessions[m.focused]
	if ok {
		m.chat.SetTimeline(&sv.Timeline)
	}
	chatLines := m.chat.Render(m.winW)
	if len(chatLines) > 0 {
		sections = append(sections, strings.Join(chatLines, "\n"))
	}

	// Spinner
	spinnerLines := m.spinner.Render(m.winW)
	if len(spinnerLines) > 0 {
		sections = append(sections, strings.Join(spinnerLines, "\n"))
	}

	// File search popup
	if m.fileSearching {
		popupLines := m.fileSearch.Render(m.winW)
		if len(popupLines) > 0 {
			sections = append(sections, strings.Join(popupLines, "\n"))
		}
	}

	// Pending messages
	pendingLines := m.pending.Render(m.winW)
	if len(pendingLines) > 0 {
		sections = append(sections, strings.Join(pendingLines, "\n"))
	}

	// Status message
	if m.statusMsg != "" {
		sections = append(sections, m.statusMsg)
	}

	// Input region (swappable slot).
	if m.activeApproval != nil {
		sections = append(sections, renderApprovalOverlay(m.activeApproval, m.winW))
	} else if m.activeModal != nil {
		modalLines := m.activeModal.Render(m.winW)
		sections = append(sections, strings.Join(modalLines, "\n"))
	} else {
		sv, svOK := m.sessions[m.focused]
		if svOK && sv.Completed {
			completedLabel := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("[Session complete]")
			sections = append(sections, completedLabel)
		} else {
			palette := m.slashPalette()
			if palette != "" {
				sections = append(sections, palette)
			}
			m.editor.SetTermHeight(m.winH)
			editorLines := m.editor.Render(m.winW)
			sections = append(sections, strings.Join(editorLines, "\n"))
		}
	}

	sections = append(sections, m.statusBar())

	return strings.Join(sections, "\n")
}

// Yolo returns current yolo mode state (exported for tests).
func (m *Model) Yolo() bool {
	return m.yolo
}

// StatusMsg returns the current transient status message (exported for tests).
func (m *Model) StatusMsg() string {
	return m.statusMsg
}

// ShowTokens returns whether the tokens overlay is displayed (exported for tests).
func (m *Model) ShowTokens() bool {
	_, ok := m.activeModal.(*TokensOverlay)
	return ok
}

// OpenTokensOverlay opens the /tokens overlay. Used in tests to set this state
// synchronously without going through the textarea input path.
func (m *Model) OpenTokensOverlay() {
	sv := m.sessions[m.focused]
	if sv == nil {
		return
	}
	m.activeModal = NewTokensOverlay(sv, m.modelName, m.contextWindow, func() { m.activeModal = nil })
}

// ShowHelp returns whether the help overlay is displayed (exported for tests).
func (m *Model) ShowHelp() bool {
	_, ok := m.activeModal.(*HelpOverlay)
	return ok
}

// OpenHelpOverlay opens the help overlay. Used in tests to set this state
// synchronously without going through the key input path.
func (m *Model) OpenHelpOverlay() {
	helpH := m.winH - stripHeight - statusHeight - 2
	if helpH < 1 {
		helpH = 1
	}
	m.activeModal = NewHelpOverlay(helpH, func() { m.activeModal = nil })
}
