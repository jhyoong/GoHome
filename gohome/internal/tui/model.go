package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
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

	// Context-fullness warning sentinels (Task 11.16).
	warned80 bool
	warned95 bool
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

	// cursor indexes the focused session's Timeline. When input is empty,
	// Up/Down move the cursor; Enter on a tool entry toggles expansion.
	cursor int

	// Phase 12 populates these; Phase 11 renders them.
	modelName     string // LLM model name; "?" when empty
	yolo          bool   // YOLO mode (skip approval)
	contextWindow int    // context window size; defaults to 128000

	// Approval overlay state (Task 11.9+).
	// activeApproval is the prompt currently displayed in the input region.
	// pendingApprovals maps sessionID -> prompt for non-focused sessions.
	activeApproval   *approvalPrompt
	pendingApprovals map[string]*approvalPrompt

	// showTokens controls the /tokens overlay (Task 11.15).
	showTokens bool

	// statusMsg is a transient message shown near the status bar (Task 11.14).
	statusMsg string

	// onYoloChange is called whenever the /yolo command toggles the YOLO flag.
	// It is set via SetYoloCallback and allows the TUI to propagate the change
	// to the guard without importing the guard package directly.
	onYoloChange func(bool)

	// Context warning tracking per session (Task 11.16).
	// warned80/warned95 are set in handleAgentEvent to fire once per session.
	contextNotice string // most recent context warning for the notification line

	// slashCB holds optional callbacks wired to slash commands (/new, /resume,
	// /model, /cancel). Set via SetSlashCallbacks.
	slashCB SlashCallbacks
}

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
		theme:            style.Default(),
		sessions:         map[string]*SessionView{sessionID: main},
		order:            []string{sessionID},
		focused:          sessionID,
		input:            ta,
		inputCh:          inputCh,
		contextWindow:    128000,
		pendingApprovals: make(map[string]*approvalPrompt),
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
// cursorActive controls whether the timeline cursor highlight is shown.
// Pass true when the input is empty (cursor-navigation mode).
func (m *Model) rebuildViewport() {
	if !m.vpReady {
		return
	}
	sv, ok := m.sessions[m.focused]
	if !ok {
		return
	}
	cur := -1
	if strings.TrimSpace(m.input.Value()) == "" {
		m.clampCursor()
		cur = m.cursor
	}
	content := renderTimeline(sv, cur)
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
			m.checkContextWarnings(sv)
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

// resolveApproval sends dec on the active approval's reply channel and clears
// the active approval. If another pending approval exists for the focused
// session, it is promoted to active.
func (m *Model) resolveApproval(dec guard.ApprovalDecision) {
	if m.activeApproval == nil {
		return
	}
	m.activeApproval.reply <- dec
	m.activeApproval = nil
	// Promote any pending approval for the now-focused session.
	m.promoteApproval()
}

// promoteApproval checks whether the focused session has a pending approval
// and, if so, sets it as the active approval.
func (m *Model) promoteApproval() {
	if m.activeApproval != nil {
		return
	}
	if ap, ok := m.pendingApprovals[m.focused]; ok {
		m.activeApproval = ap
		delete(m.pendingApprovals, m.focused)
	}
}

// notificationLine returns a warning string when a non-focused session needs
// approval (or another session is in-flight), or the highest context warning,
// or "" when quiet.
func (m *Model) notificationLine() string {
	// Pending approvals take priority.
	for sid := range m.pendingApprovals {
		if sid != m.focused {
			return fmt.Sprintf("! [%s] needs approval -- Ctrl+] to focus", sid)
		}
	}
	// Secondary: another session is in-flight while we are focused elsewhere.
	for _, id := range m.order {
		if id != m.focused {
			if sv, ok := m.sessions[id]; ok && sv.InFlight {
				return fmt.Sprintf("! [%s] is running", id)
			}
		}
	}
	// Context fullness warning for the focused session (Task 11.16).
	if m.contextNotice != "" {
		return m.contextNotice
	}
	return ""
}

// checkContextWarnings fires one-time context-fullness warnings for sv.
// It updates sv.warned80/warned95 and m.contextNotice.
func (m *Model) checkContextWarnings(sv *SessionView) {
	if m.contextWindow <= 0 {
		return
	}
	used := sv.Usage.InputTokens + sv.Usage.OutputTokens
	ratio := float64(used) / float64(m.contextWindow)
	if ratio >= 0.95 && !sv.warned95 {
		sv.warned95 = true
		m.contextNotice = "Context near limit -- next turn may fail or truncate."
	} else if ratio >= 0.80 && !sv.warned80 {
		sv.warned80 = true
		m.contextNotice = "Context 80% full -- consider /new or /resume into a fresh session."
	}
}

// vpHeight returns the viewport height given current window dimensions.
// When a notification line is visible, one extra row is consumed.
func (m *Model) vpHeight() int {
	notif := 0
	if m.notificationLine() != "" {
		notif = 1
	}
	h := m.winH - inputHeight - statusHeight - stripHeight - notif
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

	case approvalReqMsg:
		// If this is for the focused session and no approval is active, set it active.
		if msg.Req.SessionID == m.focused && m.activeApproval == nil {
			ap := newApprovalPrompt(msg.Req, msg.Reply)
			m.activeApproval = ap
		} else {
			// Queue for later (covers both non-focused and second concurrent request).
			m.pendingApprovals[msg.Req.SessionID] = newApprovalPrompt(msg.Req, msg.Reply)
		}

	case tea.KeyMsg:
		// Ctrl+C always quits.
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}

		// When an approval is active, route keys to the approval handler.
		if m.activeApproval != nil {
			cmds = append(cmds, m.handleApprovalKey(msg))
			return m, tea.Batch(cmds...)
		}

		// When the /tokens overlay is open, only Esc is handled.
		if m.showTokens {
			if msg.Type == tea.KeyEsc {
				m.showTokens = false
			}
			return m, tea.Batch(cmds...)
		}

		switch msg.Type {
		case tea.KeyEnter:
			// Alt+Enter passes through to textarea for newlines.
			if msg.Alt {
				break
			}
			text := strings.TrimSpace(m.input.Value())
			// Enter-disambiguation rule:
			// - If input is empty AND cursor is on a tool entry -> toggle expansion.
			// - If input starts with '/' -> run slash command.
			// - Otherwise -> submit text to agent.
			if text == "" {
				sv, ok := m.sessions[m.focused]
				if ok && m.cursor >= 0 && m.cursor < len(sv.Timeline) {
					entry := &sv.Timeline[m.cursor]
					if entry.Kind == "tool" {
						entry.Expanded = !entry.Expanded
						m.rebuildViewport()
					}
				}
				return m, tea.Batch(cmds...)
			}
			if strings.HasPrefix(text, "/") {
				cmd := m.handleSlashCommand(text)
				m.input.Reset()
				m.rebuildViewport()
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
				return m, tea.Batch(cmds...)
			}
			// Normal submit.
			sv := m.getOrCreateSession(m.focused, 0)
			sv.Timeline = append(sv.Timeline, TimelineEntry{
				Kind: "user",
				Text: text,
			})
			sv.InFlight = true
			m.input.Reset()
			m.cursor = len(sv.Timeline) - 1
			m.rebuildViewport()
			cmds = append(cmds, m.sendInputCmd(text))
			return m, tea.Batch(cmds...)
		}

		// PgUp / PgDn and Up/Down route to the viewport when input is non-empty.
		// When input is empty, Up/Down move the timeline cursor.
		// Ctrl+] / Ctrl+[ cycle focus between sessions.
		switch msg.Type {
		case tea.KeyCtrlCloseBracket: // Ctrl+]
			m.focusNext()
		case tea.KeyCtrlOpenBracket: // Ctrl+[
			m.focusPrev()
		case tea.KeyUp:
			if strings.TrimSpace(m.input.Value()) == "" {
				m.cursor--
				m.clampCursor()
				m.rebuildViewport()
			} else {
				var vpCmd tea.Cmd
				m.vp, vpCmd = m.vp.Update(msg)
				cmds = append(cmds, vpCmd)
			}
		case tea.KeyDown:
			if strings.TrimSpace(m.input.Value()) == "" {
				m.cursor++
				m.clampCursor()
				m.rebuildViewport()
			} else {
				var vpCmd tea.Cmd
				m.vp, vpCmd = m.vp.Update(msg)
				cmds = append(cmds, vpCmd)
			}
		case tea.KeyPgUp, tea.KeyPgDown:
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

// handleApprovalKey routes a key press when an approval prompt is active.
// It returns a Cmd (may be nil).
func (m *Model) handleApprovalKey(msg tea.KeyMsg) tea.Cmd {
	ap := m.activeApproval
	var cmds []tea.Cmd

	// --- steer sub-mode ---
	if ap.steering {
		switch msg.Type {
		case tea.KeyEnter:
			steer := strings.TrimSpace(ap.steerInput.Value())
			m.resolveApproval(guard.ApprovalDecision{
				Outcome:      guard.DenySteer,
				SteerMessage: steer,
			})
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

	// --- top-level approval menu ---
	switch {
	case msg.Type == tea.KeyEsc:
		m.resolveApproval(guard.ApprovalDecision{Outcome: guard.Deny})
	case keyRune(msg) == '1':
		m.resolveApproval(guard.ApprovalDecision{Outcome: guard.AllowOnce})
	case keyRune(msg) == '2':
		m.resolveApproval(guard.ApprovalDecision{
			Outcome:      guard.AllowAlways,
			SavedPattern: ap.pattern,
		})
	case keyRune(msg) == '3':
		m.resolveApproval(guard.ApprovalDecision{Outcome: guard.Deny})
	case keyRune(msg) == '4':
		ap.steering = true
		ap.steerInput.Focus()
	case keyRune(msg) == 'e':
		ap.editing = true
		ap.patternInput.SetValue(ap.pattern)
		ap.patternInput.Focus()
		ap.patternInput.CursorEnd()
	}
	return tea.Batch(cmds...)
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

// shortArg extracts a brief summary from a tool's InputJSON (the args).
// It delegates to shortSummary to produce a compact single-line representation.
func shortArg(inputJSON string) string {
	return shortSummary(strings.TrimSpace(inputJSON))
}

// renderTimeline converts a SessionView's timeline to plain text.
// cursor is the index of the highlighted entry (used when input is empty).
// Pass cursor = -1 to render without highlighting.
func renderTimeline(sv *SessionView, cursor int) string {
	var sb strings.Builder
	for i, e := range sv.Timeline {
		// Cursor marker: leading ">" for the selected entry.
		marker := "  "
		if i == cursor {
			marker = "> "
		}

		switch e.Kind {
		case "user":
			sb.WriteString(marker)
			sb.WriteString("you: ")
			sb.WriteString(e.Text)
			sb.WriteString("\n")
		case "assistant":
			sb.WriteString(marker)
			sb.WriteString(e.Text)
			sb.WriteString("\n")
		case "tool":
			// Collapsed line: "> <toolName> <short-arg>  ->  <short-result>"
			arg := shortArg(e.Text)
			result := shortSummary(e.ToolResult)
			collapsed := fmt.Sprintf("[tool] %s", e.ToolName)
			if arg != "" {
				collapsed += " " + arg
			}
			if result != "" {
				collapsed += "  ->  " + result
			}
			sb.WriteString(marker)
			sb.WriteString(collapsed)
			sb.WriteString("\n")
			// Expanded: show full args and full result.
			if e.Expanded {
				if e.Text != "" {
					sb.WriteString("       args: ")
					sb.WriteString(e.Text)
					sb.WriteString("\n")
				}
				if e.ToolResult != "" {
					sb.WriteString("       result:\n")
					for _, line := range strings.Split(e.ToolResult, "\n") {
						sb.WriteString("         ")
						sb.WriteString(line)
						sb.WriteString("\n")
					}
				}
			}
		case "notice":
			sb.WriteString(marker)
			sb.WriteString("[notice] ")
			sb.WriteString(e.Text)
			sb.WriteString("\n")
		}
	}
	return sb.String()
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

// slashCommands is the static list of available slash commands.
var slashCommands = []string{
	"/new", "/resume", "/yolo", "/endpoint", "/model", "/cancel", "/tokens", "/quit",
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
	case "/tokens":
		m.showTokens = true
		m.statusMsg = ""
	case "/cancel":
		if m.slashCB.CancelSession != nil {
			m.slashCB.CancelSession(m.focused)
		}
		sv := m.getOrCreateSession(m.focused, 0)
		sv.InFlight = false
		sv.Timeline = append(sv.Timeline, TimelineEntry{Kind: "notice", Text: "Cancelled."})
		m.statusMsg = "Cancelled"
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
		if len(fields) < 2 {
			m.statusMsg = "/resume: provide a session ID"
			break
		}
		sid := fields[1]
		if m.slashCB.ResumeSession != nil {
			err := m.slashCB.ResumeSession(sid)
			if err != nil {
				m.statusMsg = fmt.Sprintf("/resume: %v", err)
			} else {
				m.getOrCreateSession(sid, 0)
				m.focused = sid
				m.cursor = 0
				m.statusMsg = "Resumed: " + sid
			}
		} else {
			m.statusMsg = "/resume: not configured"
		}
	case "/model":
		if len(fields) < 2 {
			m.statusMsg = fmt.Sprintf("Current model: %s", m.modelName)
			break
		}
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
	default:
		m.statusMsg = cmd + ": unknown command"
	}
	return nil
}

// renderTokensOverlay renders the /tokens usage overlay for the focused session.
func (m *Model) renderTokensOverlay() string {
	sv, ok := m.sessions[m.focused]
	if !ok {
		return ""
	}
	u := sv.Usage
	used := u.InputTokens + u.OutputTokens
	total := m.contextWindow
	pct := 0
	if total > 0 {
		pct = int(float64(used) / float64(total) * 100)
		if pct > 100 {
			pct = 100
		}
	}
	modelName := m.modelName
	if modelName == "" {
		modelName = "?"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Token usage -- %s -- %s\n", sv.ID, modelName))
	sb.WriteString(fmt.Sprintf("  Input tokens    %d\n", u.InputTokens))
	sb.WriteString(fmt.Sprintf("  Output tokens   %d\n", u.OutputTokens))
	sb.WriteString(fmt.Sprintf("  Cache reads     %d\n", u.CacheReadTokens))
	sb.WriteString(fmt.Sprintf("  Cache writes    %d\n", u.CacheWriteTokens))
	sb.WriteString("  --------------------\n")
	sb.WriteString(fmt.Sprintf("  Total           %d / %d (%d%%)\n", used, total, pct))
	sb.WriteString("  Esc to close")
	return sb.String()
}

// slashPalette renders the autocomplete list when input starts with '/'.
// Returns "" when not applicable.
func (m *Model) slashPalette() string {
	val := m.input.Value()
	if !strings.HasPrefix(val, "/") {
		return ""
	}
	matches := slashComplete(val)
	if len(matches) == 0 {
		return ""
	}
	return strings.Join(matches, "  ")
}

// inputRegion returns the string to render in place of the normal textarea.
// When an approval prompt is active it renders the approval overlay instead.
func (m *Model) inputRegion() string {
	if m.activeApproval != nil {
		return renderApprovalOverlay(m.activeApproval, m.winW)
	}
	palette := m.slashPalette()
	if palette != "" {
		return palette + "\n" + m.input.View()
	}
	return m.input.View()
}

// View implements tea.Model.
func (m *Model) View() string {
	strip := m.sessionStrip()
	sbar := m.statusBar()

	// Status message line (transient, Task 11.14).
	statusLine := ""
	if m.statusMsg != "" {
		statusLine = m.statusMsg + "\n"
	}

	// Notification line between viewport and input region (Task 11.12).
	notif := ""
	if nl := m.notificationLine(); nl != "" {
		notif = m.theme.Notification.Render(nl) + "\n"
	}

	// /tokens overlay replaces the main content area (Task 11.15).
	if m.showTokens {
		overlay := m.renderTokensOverlay()
		return strip + "\n" + overlay + "\n" + notif + statusLine + sbar
	}

	if m.vpReady {
		return strip + "\n" + m.vp.View() + "\n" + notif + statusLine + m.inputRegion() + "\n" + sbar
	}
	// Viewport not yet sized (no WindowSizeMsg received); fall back to plain text.
	sv, ok := m.sessions[m.focused]
	if !ok {
		return strip + "\ngohome\n" + notif + statusLine + m.inputRegion() + "\n" + sbar
	}
	content := renderTimeline(sv, -1)
	if content == "" {
		content = "gohome\n"
	}
	return strip + "\n" + content + notif + statusLine + m.inputRegion() + "\n" + sbar
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
	return m.showTokens
}

// OpenTokensOverlay opens the /tokens overlay. Used in tests to set this state
// synchronously without going through the textarea input path.
func (m *Model) OpenTokensOverlay() {
	m.showTokens = true
}
