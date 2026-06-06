package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

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
	Status     string // "" | "pending" | "success" | "error" (tool entries only)
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

	editor  *EditorComponent
	chat    *ChatComponent
	spinner *SpinnerComponent
	inputCh chan string
	winW    int
	winH    int

	fileSearch    *FileSearchPopup
	fileSearching bool

	pendingMessages []string
	pending         *PendingMessagesComponent

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

	// lastCtrlC records the time of the most recent Ctrl+C press.
	// A second press within 500ms quits; a single press cancels the current turn.
	lastCtrlC time.Time

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
		inputCh:          inputCh,
		contextWindow:    128000,
		pendingApprovals: make(map[string]*approvalPrompt),
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

// rebuildViewport refreshes the chat component state from the focused session.
func (m *Model) rebuildViewport() {
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
	m.chat.ScrollToBottom()
}

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
		if n > 0 && sv.Timeline[n-1].Kind == "thinking" {
			sv.Timeline[n-1].Text += ev.ThinkingDelta
		} else {
			sv.Timeline = append(sv.Timeline, TimelineEntry{
				Kind: "thinking",
				Text: ev.ThinkingDelta,
			})
		}

	case agent.EventThinkingDone:
		// No-op: the thinking entry is already complete.

	case agent.EventTokenDelta:
		// Append to the last assistant entry if it is in-progress, else add new.
		sv.InFlight = true
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
			if sv.Timeline[i].Kind == "tool" && sv.Timeline[i].ToolResult == "" {
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
				Kind:       "tool",
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
				Kind: "user",
				Text: text,
			})
			sv.InFlight = true
			m.cursor = len(sv.Timeline) - 1
			dequeuedCmd = m.sendInputCmd(text)
		}

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

func (m *Model) cancelFocusedSession() {
	if m.slashCB.CancelSession != nil {
		m.slashCB.CancelSession(m.focused)
	}
	sv := m.sessions[m.focused]
	if sv != nil {
		sv.InFlight = false
		sv.Timeline = append(sv.Timeline, TimelineEntry{Kind: "notice", Text: "Cancelled."})
	}
	m.pendingMessages = m.pendingMessages[:0]
	m.spinner.Stop()
	m.statusMsg = "Cancelled"
	m.rebuildViewport()
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

// openExternalEditor writes the current editor content to a temp file, launches
// the user's preferred editor ($VISUAL / $EDITOR / vi), and returns a Cmd that
// sends an externalEditorMsg when the editor exits.
func (m *Model) openExternalEditor() tea.Cmd {
	content := m.editor.Value()

	tmpFile, err := os.CreateTemp("", "gohome-*.md")
	if err != nil {
		m.statusMsg = fmt.Sprintf("editor: %v", err)
		return nil
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.WriteString(content); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		m.statusMsg = fmt.Sprintf("editor: %v", err)
		return nil
	}
	_ = tmpFile.Close()

	editorCmd := os.Getenv("VISUAL")
	if editorCmd == "" {
		editorCmd = os.Getenv("EDITOR")
	}
	if editorCmd == "" {
		editorCmd = "vi"
	}

	c := exec.Command(editorCmd, tmpPath)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		defer func() { _ = os.Remove(tmpPath) }()
		if err != nil {
			return externalEditorMsg{Err: err}
		}
		data, readErr := os.ReadFile(tmpPath)
		if readErr != nil {
			return externalEditorMsg{Err: readErr}
		}
		return externalEditorMsg{Content: string(data)}
	})
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.winW = msg.Width
		m.winH = msg.Height
		m.editor.SetTermHeight(msg.Height)

	case spinnerTickMsg:
		if m.spinner.Active() {
			m.spinner.Tick()
			cmds = append(cmds, SpinnerTickCmd())
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
		if msg.Type == tea.KeyCtrlC {
			now := time.Now()
			doubleTap := now.Sub(m.lastCtrlC) < 500*time.Millisecond
			m.lastCtrlC = now

			if doubleTap {
				return m, tea.Quit
			}

			// Close approval prompt if one is active.
			if m.activeApproval != nil {
				m.resolveApproval(guard.ApprovalDecision{Outcome: guard.Deny})
				m.statusMsg = "Approval dismissed"
				return m, tea.Batch(cmds...)
			}

			// Cancel in-flight LLM turn for the focused session.
			sv := m.sessions[m.focused]
			if sv != nil && sv.InFlight {
				if m.slashCB.CancelSession != nil {
					m.slashCB.CancelSession(m.focused)
				}
				sv.InFlight = false
				sv.Timeline = append(sv.Timeline, TimelineEntry{Kind: "notice", Text: "Cancelled."})
				m.pendingMessages = m.pendingMessages[:0]
				m.spinner.Stop()
				m.statusMsg = "Cancelled — press Ctrl+C again to quit"
				return m, tea.Batch(cmds...)
			}

			// Nothing active — treat as first tap toward quit.
			m.statusMsg = "Press Ctrl+C again to quit"
			return m, tea.Batch(cmds...)
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

		if msg.Type == tea.KeyEsc && m.spinner.Active() {
			m.spinner.HandleInput(msg)
			return m, tea.Batch(cmds...)
		}

		switch msg.Type {
		case tea.KeyCtrlCloseBracket:
			m.focusNext()
		case tea.KeyCtrlOpenBracket:
			m.focusPrev()
		case tea.KeyPgUp:
			m.chat.ScrollUp(5)
		case tea.KeyPgDown:
			m.chat.ScrollDown(5)
		case tea.KeyTab:
			if m.confirmFileSearch() {
				return m, tea.Batch(cmds...)
			}
		case tea.KeyEnter:
			if msg.Alt {
				m.editor.InsertNewline()
			} else {
				if m.confirmFileSearch() {
					return m, tea.Batch(cmds...)
				}
				text := strings.TrimSpace(m.editor.Value())
				if text == "" {
					sv, ok := m.sessions[m.focused]
					if ok && m.cursor >= 0 && m.cursor < len(sv.Timeline) {
						entry := &sv.Timeline[m.cursor]
						if entry.Kind == "tool" {
							entry.Expanded = !entry.Expanded
						}
					}
				} else if strings.HasPrefix(text, "/") {
					cmd := m.handleSlashCommand(text)
					m.editor.SetValue("")
					m.rebuildViewport()
					if cmd != nil {
						cmds = append(cmds, cmd)
					}
				} else {
					sv := m.getOrCreateSession(m.focused, 0)
					if sv.InFlight {
						if len(m.pendingMessages) >= 10 {
							m.statusMsg = "Message queue full (10)"
						} else {
							m.pendingMessages = append(m.pendingMessages, text)
							m.editor.SetValue("")
						}
					} else {
						sv.Timeline = append(sv.Timeline, TimelineEntry{
							Kind: "user",
							Text: text,
						})
						sv.InFlight = true
						m.editor.SetValue("")
						m.cursor = len(sv.Timeline) - 1
						m.rebuildViewport()
						cmds = append(cmds, m.sendInputCmd(text))
					}
				}
			}
			return m, tea.Batch(cmds...)
		case tea.KeyCtrlE:
			return m, m.openExternalEditor()
		default:
			// File search navigation intercepts arrow keys and Esc.
			if m.fileSearching && m.fileSearch.visible {
				if msg.Type == tea.KeyUp {
					m.fileSearch.MoveUp()
					return m, nil
				}
				if msg.Type == tea.KeyDown {
					m.fileSearch.MoveDown()
					return m, nil
				}
				if msg.Type == tea.KeyEsc {
					m.fileSearching = false
					m.fileSearch.Hide()
					return m, nil
				}
			}
			cmd := m.editor.HandleInput(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			// Check for @-prefix file search.
			if q, ok := m.extractAtQuery(); ok && q != m.fileSearch.query {
				m.fileSearching = true
				m.fileSearch.query = q
				m.fileSearch.selected = 0
				cmds = append(cmds, searchFilesCmd(q))
			} else if !ok && m.fileSearching {
				m.fileSearching = false
				m.fileSearch.Hide()
			}
		}

	case FileSearchResultMsg:
		if m.fileSearching {
			m.fileSearch.SetResults(msg.Query, msg.Results)
		}

	case agentEventMsg:
		if cmd := m.handleAgentEvent(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case externalEditorMsg:
		if msg.Err != nil {
			m.statusMsg = fmt.Sprintf("editor: %v", msg.Err)
		} else {
			m.editor.SetValue(msg.Content)
		}

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
		m.pendingMessages = m.pendingMessages[:0]
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
	fmt.Fprintf(&sb, "Token usage -- %s -- %s\n", sv.ID, modelName)
	fmt.Fprintf(&sb, "  Input tokens    %d\n", u.InputTokens)
	fmt.Fprintf(&sb, "  Output tokens   %d\n", u.OutputTokens)
	fmt.Fprintf(&sb, "  Cache reads     %d\n", u.CacheReadTokens)
	fmt.Fprintf(&sb, "  Cache writes    %d\n", u.CacheWriteTokens)
	sb.WriteString("  --------------------\n")
	fmt.Fprintf(&sb, "  Total           %d / %d (%d%%)\n", used, total, pct)
	sb.WriteString("  Esc to close")
	return sb.String()
}

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
	return strings.Join(matches, "  ")
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

	if m.showTokens {
		sections = append(sections, m.renderTokensOverlay())
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

	// Input region
	if m.activeApproval != nil {
		sections = append(sections, renderApprovalOverlay(m.activeApproval, m.winW))
	} else {
		palette := m.slashPalette()
		if palette != "" {
			sections = append(sections, palette)
		}
		m.editor.SetTermHeight(m.winH)
		editorLines := m.editor.Render(m.winW)
		sections = append(sections, strings.Join(editorLines, "\n"))
	}

	sections = append(sections, m.statusBar())

	return strings.Join(sections, "\n")
}

// confirmFileSearch applies the selected file search result to the editor
// and closes the popup. Returns true if a selection was made.
func (m *Model) confirmFileSearch() bool {
	if !m.fileSearching || !m.fileSearch.visible {
		return false
	}
	path := m.fileSearch.SelectedPath()
	if path != "" {
		m.replaceAtQuery(path)
	}
	m.fileSearching = false
	m.fileSearch.Hide()
	return true
}

// extractAtQuery returns the word following the last '@' in the editor when that
// '@' is at the start of the input or preceded by whitespace. Returns ("", false)
// when the pattern is absent or the query contains whitespace.
func (m *Model) extractAtQuery() (string, bool) {
	val := m.editor.Value()
	idx := strings.LastIndex(val, "@")
	if idx < 0 {
		return "", false
	}
	if idx > 0 {
		prev := val[idx-1]
		if prev != ' ' && prev != '\t' && prev != '\n' {
			return "", false
		}
	}
	query := val[idx+1:]
	if strings.ContainsAny(query, " \t\n") {
		return "", false
	}
	if query == "" {
		return "", false
	}
	return query, true
}

// replaceAtQuery replaces the @<word> fragment in the editor with @replacement
// followed by a trailing space (to prevent re-triggering the search).
func (m *Model) replaceAtQuery(replacement string) {
	val := m.editor.Value()
	idx := strings.LastIndex(val, "@")
	if idx < 0 {
		return
	}
	newVal := val[:idx] + "@" + replacement + " "
	m.editor.SetValue(newVal)
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
