# TUI P1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Address the 4 remaining P1 experience gaps: tool execution rich display, bracketed paste, external editor support, and consistent context bar thresholds.

**Architecture:** Each feature is independent and self-contained. Tasks are ordered so that simpler, isolated changes come first (thresholds), then data-model changes (tool display), then new files (paste, external editor). TDD throughout.

**Tech Stack:** Go, Bubbletea v1.3.10, lipgloss, `os/exec` for external editor, raw ANSI escape sequences for bracketed paste.

---

### Task 1: Align progress bar color thresholds

**Files:**
- Modify: `gohome/internal/tui/progress.go:56-62`
- Modify: `gohome/internal/tui/progress_test.go`

**Step 1: Write the failing test**

Add a test to `progress_test.go` that asserts the color thresholds match the context warning thresholds (80%/95%):

```go
func TestProgressBarColorThresholds(t *testing.T) {
	tests := []struct {
		name      string
		used      int
		total     int
		wantColor string // ANSI color code number
	}{
		{"50% is green", 50, 100, "2"},
		{"79% is green", 79, 100, "2"},
		{"80% is yellow", 80, 100, "3"},
		{"90% is yellow", 90, 100, "3"},
		{"95% is yellow", 95, 100, "3"},
		{"96% is red", 96, 100, "1"},
		{"100% is red", 100, 100, "1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bar := progressBar(tc.used, tc.total, 10)
			wantSeq := "[38;5;" + tc.wantColor + "m"
			if !strings.Contains(bar, wantSeq) {
				t.Errorf("progressBar(%d, %d, 10) = %q, want color %s",
					tc.used, tc.total, bar, tc.wantColor)
			}
		})
	}
}
```

Add `"strings"` to the import block in `progress_test.go`.

**Step 2: Run test to verify it fails**

Run: `cd gohome && go test ./internal/tui/ -run TestProgressBarColorThresholds -v`
Expected: FAIL -- 50% will match green, but 80% will be red instead of yellow, and 95% will be red instead of yellow.

**Step 3: Update threshold values**

In `progress.go`, change the switch cases at lines 56-62:

```go
	switch {
	case ratio <= 0.80:
		color = lipgloss.Color("2") // green
	case ratio <= 0.95:
		color = lipgloss.Color("3") // yellow
	default:
		color = lipgloss.Color("1") // red
	}
```

**Step 4: Run test to verify it passes**

Run: `cd gohome && go test ./internal/tui/ -run TestProgressBarColor -v`
Expected: PASS

**Step 5: Run all progress tests**

Run: `cd gohome && go test ./internal/tui/ -run TestProgressBar -v`
Expected: All PASS

**Step 6: Commit**

```bash
git add gohome/internal/tui/progress.go gohome/internal/tui/progress_test.go
git commit -m "fix(tui): align progress bar color thresholds to 80%/95% warnings"
```

---

### Task 2: Add Status field to TimelineEntry and wire event flow

**Files:**
- Modify: `gohome/internal/tui/model.go:16-22` (TimelineEntry struct)
- Modify: `gohome/internal/tui/model.go:204-271` (handleAgentEvent)
- Modify: `gohome/internal/tui/style/style.go`

**Step 1: Write the failing test**

Add to `chat_test.go`:

```go
func TestToolStatusPending(t *testing.T) {
	entries := []TimelineEntry{{
		Kind:     "tool",
		ToolName: "bash",
		Text:     `{"command":"ls"}`,
		Status:   "pending",
	}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "bash") {
		t.Errorf("tool name not found: %q", joined)
	}
}

func TestToolStatusSuccess(t *testing.T) {
	entries := []TimelineEntry{{
		Kind:       "tool",
		ToolName:   "bash",
		Text:       `{"command":"ls"}`,
		ToolResult: "file.txt",
		Status:     "success",
	}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "bash") {
		t.Errorf("tool name not found: %q", joined)
	}
}

func TestToolStatusError(t *testing.T) {
	entries := []TimelineEntry{{
		Kind:       "tool",
		ToolName:   "bash",
		Text:       `{"command":"rm /"}`,
		ToolResult: "permission denied",
		Status:     "error",
	}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "ERROR") {
		t.Errorf("error prefix not found: %q", joined)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd gohome && go test ./internal/tui/ -run TestToolStatus -v`
Expected: FAIL -- `Status` field does not exist yet.

**Step 3: Add Status field to TimelineEntry**

In `model.go`, add the field to the `TimelineEntry` struct:

```go
type TimelineEntry struct {
	Kind       string // "user" | "assistant" | "tool" | "notice"
	Text       string
	ToolName   string
	ToolResult string
	Expanded   bool
	Status     string // "" | "pending" | "success" | "error" (tool entries only)
}
```

**Step 4: Add theme styles**

In `style/style.go`, replace the `ToolLine` field with three status-aware styles:

```go
type Theme struct {
	UserMsg      lipgloss.Style
	AssistantMsg lipgloss.Style
	ToolPending  lipgloss.Style
	ToolSuccess  lipgloss.Style
	ToolError    lipgloss.Style
	StatusBar    lipgloss.Style
	Notification lipgloss.Style
}
```

Update `Default()`:

```go
func Default() Theme {
	return Theme{
		UserMsg: lipgloss.NewStyle().
			Foreground(lipgloss.Color("12")).
			Bold(true),
		AssistantMsg: lipgloss.NewStyle().
			Foreground(lipgloss.Color("7")),
		ToolPending: lipgloss.NewStyle().
			Foreground(lipgloss.Color("3")).
			Italic(true),
		ToolSuccess: lipgloss.NewStyle().
			Foreground(lipgloss.Color("2")),
		ToolError: lipgloss.NewStyle().
			Foreground(lipgloss.Color("1")).
			Bold(true),
		StatusBar: lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")).
			Background(lipgloss.Color("0")),
		Notification: lipgloss.NewStyle().
			Foreground(lipgloss.Color("11")),
	}
}
```

**Step 5: Fix all compile errors referencing ToolLine**

Search for `ToolLine` or `toolStyle` in the codebase and replace with the appropriate status style. The `toolStyle` var in `chat.go` should be removed since `renderToolLine` will now select the style based on `Status`.

**Step 6: Update renderToolLine to use status-aware styles**

In `chat.go`, replace the `toolStyle` var and update `renderToolLine`:

```go
func renderToolLine(e TimelineEntry, maxWidth int) string {
	arg := shortArg(e.Text)
	result := shortSummary(e.ToolResult)

	var st lipgloss.Style
	switch e.Status {
	case "error":
		st = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	case "success":
		st = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	default: // "pending" or ""
		st = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Italic(true)
	}

	line := st.Render(fmt.Sprintf("[tool] %s", e.ToolName))
	if arg != "" {
		line += " " + arg
	}
	if e.Status == "error" && result != "" {
		line += "  ->  ERROR: " + result
	} else if result != "" {
		line += "  ->  " + result
	}
	if VisualWidth(StripAnsi(line)) > maxWidth {
		line = TruncateText(line, maxWidth)
	}
	return line
}
```

**Step 7: Wire status in handleAgentEvent**

In `model.go`, update the `EventToolCallDone` case to set `Status: "pending"`:

```go
	case agent.EventToolCallDone:
		sv.Timeline = append(sv.Timeline, TimelineEntry{
			Kind:     "tool",
			ToolName: ev.ToolName,
			Text:     ev.InputJSON,
			Status:   "pending",
		})
```

Update the `EventToolResult` case to set status based on `IsError`:

```go
	case agent.EventToolResult:
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
```

**Step 8: Run tests to verify they pass**

Run: `cd gohome && go test ./internal/tui/ -run TestToolStatus -v`
Expected: PASS

**Step 9: Run all TUI tests**

Run: `cd gohome && go test ./internal/tui/... -v`
Expected: All PASS. If any test references `ToolLine` style, fix it.

**Step 10: Commit**

```bash
git add gohome/internal/tui/model.go gohome/internal/tui/chat.go gohome/internal/tui/style/style.go gohome/internal/tui/chat_test.go
git commit -m "feat(tui): tool execution status colors (pending/success/error)"
```

---

### Task 3: EditorComponent.InsertText method

**Files:**
- Modify: `gohome/internal/tui/editor.go`
- Modify: `gohome/internal/tui/editor_test.go`

**Step 1: Write the failing test**

Add to `editor_test.go`:

```go
func TestEditorInsertTextSingleLine(t *testing.T) {
	e := NewEditor(80, 24)
	e.InsertText("hello")
	if e.Value() != "hello" {
		t.Errorf("Value() = %q, want %q", e.Value(), "hello")
	}
}

func TestEditorInsertTextMultiLine(t *testing.T) {
	e := NewEditor(80, 24)
	e.InsertText("line1\nline2\nline3")
	if e.Value() != "line1\nline2\nline3" {
		t.Errorf("Value() = %q, want %q", e.Value(), "line1\nline2\nline3")
	}
}

func TestEditorInsertTextAtCursor(t *testing.T) {
	e := NewEditor(80, 24)
	e.InsertRune('a')
	e.InsertRune('b')
	// cursor is after 'b', col=2
	e.InsertText("X\nY")
	// expected: "abX\nY"
	want := "abX\nY"
	if e.Value() != want {
		t.Errorf("Value() = %q, want %q", e.Value(), want)
	}
}

func TestEditorInsertTextStripsCarriageReturn(t *testing.T) {
	e := NewEditor(80, 24)
	e.InsertText("line1\r\nline2\r\n")
	want := "line1\nline2\n"
	if e.Value() != want {
		t.Errorf("Value() = %q, want %q", e.Value(), want)
	}
}

func TestEditorInsertTextReplacesTab(t *testing.T) {
	e := NewEditor(80, 24)
	e.InsertText("a\tb")
	want := "a    b"
	if e.Value() != want {
		t.Errorf("Value() = %q, want %q", e.Value(), want)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd gohome && go test ./internal/tui/ -run TestEditorInsertText -v`
Expected: FAIL -- method does not exist.

**Step 3: Implement InsertText**

Add to `editor.go`:

```go
// InsertText inserts a block of text at the current cursor position.
// Carriage returns are stripped and tabs are replaced with 4 spaces.
// Multi-line text splits the current line at the cursor; the first fragment
// is appended to the current line and subsequent lines are inserted after it.
func (e *EditorComponent) InsertText(s string) {
	if e.browsing {
		e.history.StopBrowsing()
		e.browsing = false
	}

	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\t", "    ")

	parts := strings.Split(s, "\n")
	if len(parts) == 0 {
		return
	}

	line := []rune(e.lines[e.cursorLine])
	col := e.cursorCol
	if col > len(line) {
		col = len(line)
	}

	before := string(line[:col])
	after := string(line[col:])

	if len(parts) == 1 {
		e.lines[e.cursorLine] = before + parts[0] + after
		e.cursorCol = col + utf8.RuneCountInString(parts[0])
		return
	}

	// First part appends to current line's prefix.
	e.lines[e.cursorLine] = before + parts[0]

	// Middle parts are new lines.
	newLines := make([]string, 0, len(e.lines)+len(parts)-1)
	newLines = append(newLines, e.lines[:e.cursorLine+1]...)
	for _, p := range parts[1 : len(parts)-1] {
		newLines = append(newLines, p)
	}

	// Last part gets the suffix from the original line.
	lastPart := parts[len(parts)-1]
	newLines = append(newLines, lastPart+after)
	newLines = append(newLines, e.lines[e.cursorLine+1:]...)

	e.lines = newLines
	e.cursorLine = e.cursorLine + len(parts) - 1
	e.cursorCol = utf8.RuneCountInString(lastPart)
	e.clampScroll()
}
```

**Step 4: Run tests to verify they pass**

Run: `cd gohome && go test ./internal/tui/ -run TestEditorInsertText -v`
Expected: PASS

**Step 5: Run all editor tests**

Run: `cd gohome && go test ./internal/tui/ -run TestEditor -v`
Expected: All PASS

**Step 6: Commit**

```bash
git add gohome/internal/tui/editor.go gohome/internal/tui/editor_test.go
git commit -m "feat(tui): EditorComponent.InsertText for multi-line paste insertion"
```

---

### Task 4: Bracketed paste support

**Files:**
- Create: `gohome/internal/tui/paste.go`
- Create: `gohome/internal/tui/paste_test.go`
- Modify: `gohome/internal/tui/model.go`
- Modify: `gohome/cmd/gohome/main.go:248`

**Step 1: Write the failing test for PasteReader**

Create `paste_test.go`:

```go
package tui

import (
	"bytes"
	"io"
	"testing"
)

func TestPasteReaderPassesNormalInput(t *testing.T) {
	input := []byte("hello")
	pr := NewPasteReader(bytes.NewReader(input))
	buf := make([]byte, 256)
	n, err := pr.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if string(buf[:n]) != "hello" {
		t.Errorf("got %q, want %q", buf[:n], "hello")
	}
}

func TestPasteReaderExtractsPaste(t *testing.T) {
	// Simulate a bracketed paste: start marker + content + end marker
	input := []byte("\x1b[200~pasted text\x1b[201~")
	pr := NewPasteReader(bytes.NewReader(input))
	// The reader should strip the paste and instead deliver nothing to
	// the normal read path; the paste goes to PasteContent().
	buf := make([]byte, 256)
	n, _ := pr.Read(buf)
	if n != 0 {
		t.Errorf("expected 0 bytes from Read during paste, got %d: %q", n, buf[:n])
	}
	got := pr.PasteContent()
	if got != "pasted text" {
		t.Errorf("PasteContent() = %q, want %q", got, "pasted text")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd gohome && go test ./internal/tui/ -run TestPasteReader -v`
Expected: FAIL -- file and types do not exist.

**Step 3: Implement PasteReader**

Create `paste.go`:

```go
package tui

import (
	"bytes"
	"io"

	tea "github.com/charmbracelet/bubbletea"
)

var (
	pasteStart = []byte("\x1b[200~")
	pasteEnd   = []byte("\x1b[201~")
)

// PasteMsg is sent when a bracketed paste is detected.
type PasteMsg struct {
	Text string
}

// PasteReader wraps an io.Reader and detects bracketed paste sequences.
// When a paste bracket is detected, the paste content is buffered and
// made available via PasteContent(). Normal bytes pass through unchanged.
type PasteReader struct {
	inner   io.Reader
	buf     []byte
	pasting bool
	paste   bytes.Buffer
	ready   string
}

// NewPasteReader wraps r with bracketed paste detection.
func NewPasteReader(r io.Reader) *PasteReader {
	return &PasteReader{inner: r}
}

// PasteContent returns the most recent paste content and clears it.
func (pr *PasteReader) PasteContent() string {
	s := pr.ready
	pr.ready = ""
	return s
}

// Read implements io.Reader. It detects paste bracket sequences in the
// byte stream. Paste content is buffered internally; call PasteContent()
// to retrieve it.
func (pr *PasteReader) Read(p []byte) (int, error) {
	tmp := make([]byte, len(p))
	n, err := pr.inner.Read(tmp)
	if n == 0 {
		return 0, err
	}
	data := tmp[:n]

	// If we're inside a paste bracket, buffer everything until end marker.
	if pr.pasting {
		if idx := bytes.Index(data, pasteEnd); idx >= 0 {
			pr.paste.Write(data[:idx])
			pr.ready = pr.paste.String()
			pr.paste.Reset()
			pr.pasting = false
			// Anything after the end marker goes to output.
			rest := data[idx+len(pasteEnd):]
			copy(p, rest)
			return len(rest), err
		}
		pr.paste.Write(data)
		return 0, err
	}

	// Check for paste start marker.
	if idx := bytes.Index(data, pasteStart); idx >= 0 {
		pr.pasting = true
		// Bytes before the marker are normal output.
		out := data[:idx]
		// Bytes after the marker are paste content.
		after := data[idx+len(pasteStart):]

		// Check if end marker is also in this chunk.
		if endIdx := bytes.Index(after, pasteEnd); endIdx >= 0 {
			pr.paste.Write(after[:endIdx])
			pr.ready = pr.paste.String()
			pr.paste.Reset()
			pr.pasting = false
			rest := after[endIdx+len(pasteEnd):]
			out = append(out, rest...)
		} else {
			pr.paste.Write(after)
		}

		copy(p, out)
		return len(out), err
	}

	// No paste markers, pass through.
	copy(p, data)
	return n, err
}

// EnableBracketedPaste returns the ANSI sequence to enable bracketed paste mode.
func EnableBracketedPaste() string {
	return "\x1b[?2004h"
}

// DisableBracketedPaste returns the ANSI sequence to disable bracketed paste mode.
func DisableBracketedPaste() string {
	return "\x1b[?2004l"
}
```

**Step 4: Run tests to verify they pass**

Run: `cd gohome && go test ./internal/tui/ -run TestPasteReader -v`
Expected: PASS

**Step 5: Commit PasteReader**

```bash
git add gohome/internal/tui/paste.go gohome/internal/tui/paste_test.go
git commit -m "feat(tui): PasteReader for bracketed paste detection"
```

**Step 6: Wire PasteReader into the program**

However, Bubbletea's `tea.WithInput` expects an `io.Reader`, and the `PasteReader` needs to communicate paste events back to the model. The cleanest approach is: instead of a custom io.Reader, handle Bubbletea's built-in paste support.

**Important note for the implementer:** Bubbletea v1.3.10 may already have bracketed paste support internally. Check if `tea.KeyMsg` has a `Paste` field or if there is a `tea.WithBracketedPaste()` option. If Bubbletea already handles this, skip the custom PasteReader and use the built-in mechanism instead. If not, proceed with the PasteReader approach.

**Alternative integration if Bubbletea lacks built-in support:**

In `gohome/cmd/gohome/main.go`, wrap stdin:

```go
pasteReader := tui.NewPasteReader(os.Stdin)
p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithInput(pasteReader))
```

Then in `model.go`, add a `pasteReader *PasteReader` field to `Model` and check for paste content on each Update:

```go
// In Update, after handling all other messages, check for paste.
if m.pasteReader != nil {
    if content := m.pasteReader.PasteContent(); content != "" {
        m.editor.InsertText(content)
    }
}
```

Add `Init()` to write the enable sequence:

```go
func (m *Model) Init() tea.Cmd {
    return tea.Println(EnableBracketedPaste())
}
```

The disable sequence should be written on quit/cleanup.

**Step 7: Write integration test**

Add to `paste_test.go`:

```go
func TestPasteReaderMultiChunkPaste(t *testing.T) {
	// Paste split across two reads.
	r, w := io.Pipe()
	pr := NewPasteReader(r)

	go func() {
		w.Write([]byte("\x1b[200~first"))
		w.Write([]byte(" second\x1b[201~"))
		w.Close()
	}()

	buf := make([]byte, 256)
	// First read: start of paste, no output.
	n, _ := pr.Read(buf)
	if n != 0 {
		t.Errorf("first read: got %d bytes, want 0", n)
	}

	// Second read: end of paste, no output.
	n, _ = pr.Read(buf)
	if n != 0 {
		t.Errorf("second read: got %d bytes, want 0", n)
	}

	got := pr.PasteContent()
	if got != "first second" {
		t.Errorf("PasteContent() = %q, want %q", got, "first second")
	}
}
```

**Step 8: Run all paste tests**

Run: `cd gohome && go test ./internal/tui/ -run TestPaste -v`
Expected: All PASS

**Step 9: Commit integration wiring**

```bash
git add gohome/internal/tui/paste.go gohome/internal/tui/paste_test.go gohome/internal/tui/model.go gohome/cmd/gohome/main.go
git commit -m "feat(tui): wire bracketed paste into editor via PasteReader"
```

---

### Task 5: External editor support

**Files:**
- Modify: `gohome/internal/tui/model.go`
- Modify: `gohome/internal/tui/tui_test.go` (or create a new test file)

**Step 1: Add the message type**

Add to `model.go` near the other message types:

```go
// externalEditorMsg is sent when the external editor exits.
type externalEditorMsg struct {
	Content string
	Err     error
}
```

**Step 2: Add the Ctrl+E handler**

In `model.go`, inside the `tea.KeyMsg` switch in `Update()`, add a case before the `default` that forwards to the editor. The key must be checked when no approval is active and no token overlay is shown (these are already guarded above the switch):

```go
		case tea.KeyCtrlE:
			return m, m.openExternalEditor()
```

Implement `openExternalEditor`:

```go
// openExternalEditor writes the current editor content to a temp file,
// launches $VISUAL/$EDITOR/vi, and returns a Cmd that suspends the TUI.
func (m *Model) openExternalEditor() tea.Cmd {
	content := m.editor.Value()

	tmpFile, err := os.CreateTemp("", "gohome-*.md")
	if err != nil {
		m.statusMsg = fmt.Sprintf("editor: %v", err)
		return nil
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		m.statusMsg = fmt.Sprintf("editor: %v", err)
		return nil
	}
	tmpFile.Close()

	editorCmd := os.Getenv("VISUAL")
	if editorCmd == "" {
		editorCmd = os.Getenv("EDITOR")
	}
	if editorCmd == "" {
		editorCmd = "vi"
	}

	c := exec.Command(editorCmd, tmpPath)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		defer os.Remove(tmpPath)
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
```

Add `"os"` and `"os/exec"` to the import block in `model.go`.

**Step 3: Handle externalEditorMsg in Update**

Add a case in the `Update()` switch:

```go
	case externalEditorMsg:
		if msg.Err != nil {
			m.statusMsg = fmt.Sprintf("editor: %v", msg.Err)
		} else {
			m.editor.SetValue(msg.Content)
		}
```

**Step 4: Write a unit test for the message handler**

Add to `tui_test.go` (or a new `editor_external_test.go`):

```go
func TestExternalEditorMsgSetsContent(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	// Simulate the message that would come back from the external editor.
	tm.Send(tui.ExternalEditorMsg{Content: "edited content", Err: nil})

	// The editor should now contain the text.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("edited content"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
```

**Note:** You will need to export `ExternalEditorMsg` (capitalize it) and add an alias like the pattern used for `AgentEventMsg`/`agentEventMsg`:

```go
type ExternalEditorMsg = externalEditorMsg
```

**Step 5: Run the test**

Run: `cd gohome && go test ./internal/tui/ -run TestExternalEditor -v`
Expected: PASS

**Step 6: Run all TUI tests**

Run: `cd gohome && go test ./internal/tui/... -v`
Expected: All PASS

**Step 7: Commit**

```bash
git add gohome/internal/tui/model.go gohome/internal/tui/tui_test.go
git commit -m "feat(tui): Ctrl+E opens external editor ($VISUAL/$EDITOR/vi)"
```

---

### Task 6: Final integration test and cleanup

**Files:**
- All modified files from tasks 1-5

**Step 1: Run the full test suite**

Run: `cd gohome && go test ./... -v`
Expected: All PASS

**Step 2: Run go vet**

Run: `cd gohome && go vet ./...`
Expected: No issues

**Step 3: Check for any remaining references to the old ToolLine style**

Run: `grep -rn "ToolLine\|toolStyle" gohome/internal/tui/`
Expected: No matches (all replaced with ToolPending/ToolSuccess/ToolError)

**Step 4: Commit any cleanup if needed**

```bash
git add -A
git commit -m "chore(tui): P1 cleanup"
```
