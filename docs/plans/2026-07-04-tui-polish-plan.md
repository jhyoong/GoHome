# TUI Polish Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Improve GoHome TUI user-friendliness by making tool calls, user messages, thinking blocks, and truncated output more readable.

**Architecture:** Four surgical changes to the rendering layer in `gohome/internal/tui/chat.go` and the event handler in `model_agent.go`. No new packages or structural refactoring.

**Tech Stack:** Go, lipgloss (styling), goldmark (markdown), charmbracelet/x/exp/golden (snapshot tests).

---

### Task 1: Tool Call Headers -- Contextual Short Form

**Files:**
- Modify: `gohome/internal/tui/chat.go:585-612` (replace `renderToolLine`)
- Test: `gohome/internal/tui/tui_snapshot_test.go` (existing snapshot tests)
- Golden: `gohome/internal/tui/testdata/TestSnapshots/*.golden` (regenerate)

**Step 1: Write a unit test for the new `renderToolSummary` function**

Create a table-driven test in `gohome/internal/tui/chat_test.go` (or add to existing test file):

```go
func TestRenderToolSummary(t *testing.T) {
	tests := []struct {
		name     string
		entry    TimelineEntry
		maxWidth int
		wantSub  string // substring that must appear in output
	}{
		{
			name: "bash tool shows dollar prefix",
			entry: TimelineEntry{
				Kind:     KindTool,
				ToolName: "bash",
				Text:     `{"command":"ls -la"}`,
				Status:   "success",
			},
			maxWidth: 80,
			wantSub:  "$ ls -la",
		},
		{
			name: "read tool shows filepath only",
			entry: TimelineEntry{
				Kind:     KindTool,
				ToolName: "read",
				Text:     `{"file_path":"src/main.go"}`,
				Status:   "success",
			},
			maxWidth: 80,
			wantSub:  "src/main.go",
		},
		{
			name: "write tool shows write prefix",
			entry: TimelineEntry{
				Kind:     KindTool,
				ToolName: "write",
				Text:     `{"file_path":"out.txt","content":"hello"}`,
				Status:   "success",
			},
			maxWidth: 80,
			wantSub:  "write out.txt",
		},
		{
			name: "edit tool shows edit prefix",
			entry: TimelineEntry{
				Kind:     KindTool,
				ToolName: "edit",
				Text:     `{"file_path":"src/app.go","old_string":"foo","new_string":"bar"}`,
				Status:   "success",
			},
			maxWidth: 80,
			wantSub:  "edit src/app.go",
		},
		{
			name: "subagent shows prompt summary",
			entry: TimelineEntry{
				Kind:     KindTool,
				ToolName: "subagent",
				Text:     `{"prompt":"Investigate the failing test in auth module"}`,
				Status:   "pending",
			},
			maxWidth: 80,
			wantSub:  "subagent: Investigate the failing test in auth module",
		},
		{
			name: "unknown tool shows toolname colon arg",
			entry: TimelineEntry{
				Kind:     KindTool,
				ToolName: "grep",
				Text:     `{"pattern":"TODO"}`,
				Status:   "success",
			},
			maxWidth: 80,
			wantSub:  `grep: {"pattern":"TODO"}`,
		},
		{
			name: "bash with result arrow",
			entry: TimelineEntry{
				Kind:       KindTool,
				ToolName:   "bash",
				Text:       `{"command":"find . -name '*.go'"}`,
				ToolResult: "a\nb\nc\nd\ne\nf",
				Status:     "success",
			},
			maxWidth: 80,
			wantSub:  "$ find . -name '*.go'",
		},
		{
			name: "invalid json falls back to shortSummary",
			entry: TimelineEntry{
				Kind:     KindTool,
				ToolName: "bash",
				Text:     `not json`,
				Status:   "pending",
			},
			maxWidth: 80,
			wantSub:  "$ not json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderToolSummary(tt.entry, tt.maxWidth)
			plain := StripAnsi(got)
			if !strings.Contains(plain, tt.wantSub) {
				t.Errorf("renderToolSummary() = %q, want substring %q", plain, tt.wantSub)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/tui/ -run TestRenderToolSummary -v`
Expected: FAIL -- `renderToolSummary` undefined.

**Step 3: Implement `renderToolSummary`**

Replace `renderToolLine` in `gohome/internal/tui/chat.go:585-612` with:

```go
// renderToolSummary builds the collapsed single-line representation of a tool entry.
// Displays a contextual short form per tool type instead of raw JSON.
func renderToolSummary(e TimelineEntry, maxWidth int) string {
	arg := extractToolArg(e.ToolName, e.Text)
	result := shortSummary(e.ToolResult)

	var st lipgloss.Style
	switch e.Status {
	case "error":
		st = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	case "success":
		st = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	default:
		st = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Italic(true)
	}

	var label string
	switch e.ToolName {
	case "bash":
		label = "$ " + arg
	case "read":
		label = arg
	case "write":
		label = "write " + arg
	case "edit":
		label = "edit " + arg
	case "subagent":
		label = "subagent: " + arg
	default:
		label = e.ToolName + ": " + arg
	}

	line := st.Render(label)
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

// extractToolArg parses the JSON input and returns the key argument for display.
func extractToolArg(toolName, inputJSON string) string {
	inputJSON = strings.TrimSpace(inputJSON)
	if inputJSON == "" {
		return ""
	}

	var key string
	switch toolName {
	case "bash":
		key = "command"
	case "read", "write", "edit":
		key = "file_path"
	case "subagent":
		key = "prompt"
	default:
		return shortSummary(inputJSON)
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(inputJSON), &m); err != nil {
		return shortSummary(inputJSON)
	}
	val, ok := m[key]
	if !ok {
		return shortSummary(inputJSON)
	}
	s, ok := val.(string)
	if !ok {
		return shortSummary(inputJSON)
	}
	return shortSummary(s)
}
```

Add `"encoding/json"` to the imports in `chat.go`.

**Step 4: Update all callers of `renderToolLine` to use `renderToolSummary`**

In `chat.go`, find and replace:
- Line ~409: `line := renderToolLine(*e, maxWidth-6)` -> `line := renderToolSummary(*e, maxWidth-6)`
- Line ~440: `line := renderToolLine(*e, maxWidth-2)` -> `line := renderToolSummary(*e, maxWidth-2)`

Also update the `normEditPath` regex in `tui_snapshot_test.go` since the format changed from `[tool] edit {` to `edit src/...`.

**Step 5: Run tests to verify they pass**

Run: `go test ./gohome/internal/tui/ -run TestRenderToolSummary -v`
Expected: PASS

**Step 6: Regenerate golden snapshot files**

Run: `go test ./gohome/internal/tui/ -run TestSnapshots -update`
Expected: golden files regenerated without error.

**Step 7: Run full test suite**

Run: `go test ./gohome/internal/tui/ -v`
Expected: all PASS.

**Step 8: Commit**

```bash
git add gohome/internal/tui/chat.go gohome/internal/tui/chat_test.go gohome/internal/tui/tui_snapshot_test.go gohome/internal/tui/testdata/
git commit -m "feat(tui): contextual tool call headers -- show '$ cmd' instead of '[tool] bash {...}'"
```

---

### Task 2: User Message Blocks -- Full-Width Background

**Files:**
- Modify: `gohome/internal/tui/chat.go:362-375` (KindUser branch in `renderEntry`)
- Modify: `gohome/internal/tui/chat.go:12-24` (style vars section -- add `userBlockStyle`)
- Modify: `gohome/internal/tui/chat.go:154-156` (entryLineCount KindUser branch)
- Test: `gohome/internal/tui/tui_snapshot_test.go`
- Golden: `gohome/internal/tui/testdata/TestSnapshots/after_user_message.golden`

**Step 1: Write a unit test for user block rendering**

Add to an existing test file (or create `chat_render_test.go`):

```go
func TestUserBlockRendering(t *testing.T) {
	timeline := []TimelineEntry{
		{Kind: KindUser, Text: "hello world"},
	}
	chat := NewChat(&timeline, 24)
	lines := chat.Render(80)

	// Should NOT contain "you:" prefix
	for _, l := range lines {
		if strings.Contains(l, "you:") {
			t.Errorf("user block should not contain 'you:' prefix, got: %q", l)
		}
	}
	// Should contain the user text
	found := false
	for _, l := range lines {
		if strings.Contains(l, "hello world") {
			found = true
			break
		}
	}
	if !found {
		t.Error("user block should contain the message text")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/tui/ -run TestUserBlockRendering -v`
Expected: FAIL -- contains "you:" prefix.

**Step 3: Implement user block style**

In `chat.go`, replace the `userPrefix` style var and update the KindUser rendering:

Add a new style in the var block (replacing `userPrefix`):

```go
var (
	userBlockStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		PaddingLeft(1).
		BorderStyle(lipgloss.ThickBorder()).
		BorderLeft(true).
		BorderForeground(lipgloss.Color("12"))
	noticeStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	expandedBg     = lipgloss.NewStyle().Background(lipgloss.Color("236"))
	// ... rest unchanged
)
```

Replace the `KindUser` case in `renderEntry` (~line 365):

```go
case KindUser:
	text := WrapText(e.Text, maxWidth-6)
	for _, l := range text {
		styled := userBlockStyle.Width(maxWidth - 4).Render(l)
		lines = append(lines, marker+styled)
	}
```

Update the `entryLineCount` KindUser case (~line 155) to match:

```go
case KindUser:
	return len(WrapText(e.Text, maxWidth-6))
```

Also update the `countLines` KindUser case (~line 231):

```go
case KindUser:
	count += len(WrapText(e.Text, maxWidth-6))
```

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/tui/ -run TestUserBlockRendering -v`
Expected: PASS

**Step 5: Regenerate golden files and run full suite**

Run: `go test ./gohome/internal/tui/ -run TestSnapshots -update && go test ./gohome/internal/tui/ -v`
Expected: all PASS.

**Step 6: Commit**

```bash
git add gohome/internal/tui/chat.go gohome/internal/tui/chat_test.go gohome/internal/tui/testdata/
git commit -m "feat(tui): full-width background block for user messages"
```

---

### Task 3: Thinking Merge + Always Visible

**Files:**
- Modify: `gohome/internal/tui/model_agent.go:35-40` (remove `EventThinkingDone` collapse)
- Modify: `gohome/internal/tui/chat.go:389-404` (KindThinking `renderEntry` -- remove collapsed branch)
- Modify: `gohome/internal/tui/chat.go:163-170` (KindThinking `entryLineCount` -- remove collapsed branch)
- Modify: `gohome/internal/tui/chat.go:239-248` (KindThinking `countLines` -- remove collapsed branch)
- Test: `gohome/internal/tui/tui_snapshot_test.go`

**Step 1: Write a test for thinking merge and always-visible behavior**

```go
func TestThinkingMergeAndAlwaysVisible(t *testing.T) {
	m := newSized()

	// Send two consecutive thinking deltas.
	m = apply(m, tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:          agent.EventThinkingDelta,
		SessionID:     "main",
		ThinkingDelta: "First thought.",
	}})
	m = apply(m, tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:          agent.EventThinkingDelta,
		SessionID:     "main",
		ThinkingDelta: " Second thought.",
	}})

	// Should have merged into one timeline entry.
	sv := m.GetSession("main")
	thinkCount := 0
	for _, e := range sv.Timeline {
		if e.Kind == tui.KindThinking {
			thinkCount++
		}
	}
	if thinkCount != 1 {
		t.Errorf("expected 1 thinking entry, got %d", thinkCount)
	}

	// After EventThinkingDone, content should still be visible (not collapsed).
	m = apply(m, tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventThinkingDone,
		SessionID: "main",
	}})

	view := m.View()
	if !strings.Contains(view, "First thought.") {
		t.Error("thinking content should be visible after ThinkingDone")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/tui/ -run TestThinkingMergeAndAlwaysVisible -v`
Expected: FAIL -- thinking content not visible after ThinkingDone (collapsed).

**Step 3: Remove auto-collapse in `model_agent.go`**

In `model_agent.go`, remove or no-op the `EventThinkingDone` handler (lines 35-40). Replace with:

```go
case agent.EventThinkingDone:
	// No-op: thinking entries stay visible.
```

**Step 4: Simplify KindThinking rendering in `chat.go`**

Replace the `KindThinking` case in `renderEntry` (~line 389-404):

```go
case KindThinking:
	mdLines := RenderMarkdown(e.Text, maxWidth-4)
	if len(mdLines) == 0 {
		mdLines = WrapText(e.Text, maxWidth-4)
	}
	for j, l := range mdLines {
		styled := ansiDim + ansiItalic + l + ansiReset
		if j == 0 {
			lines = append(lines, marker+"  "+styled)
		} else {
			lines = append(lines, "    "+styled)
		}
	}
```

Replace the `KindThinking` case in `entryLineCount` (~line 163-170):

```go
case KindThinking:
	lines := RenderMarkdown(e.Text, maxWidth-4)
	if len(lines) == 0 {
		lines = WrapText(e.Text, maxWidth-4)
	}
	return len(lines)
```

Replace the `KindThinking` case in `countLines` (~line 239-248):

```go
case KindThinking:
	mdl := RenderMarkdown(e.Text, maxWidth-4)
	if len(mdl) == 0 {
		mdl = WrapText(e.Text, maxWidth-4)
	}
	count += len(mdl)
```

**Step 5: Run test to verify it passes**

Run: `go test ./gohome/internal/tui/ -run TestThinkingMergeAndAlwaysVisible -v`
Expected: PASS

**Step 6: Regenerate golden files and run full suite**

Run: `go test ./gohome/internal/tui/ -run TestSnapshots -update && go test ./gohome/internal/tui/ -v`
Expected: all PASS.

**Step 7: Commit**

```bash
git add gohome/internal/tui/chat.go gohome/internal/tui/model_agent.go gohome/internal/tui/tui_snapshot_test.go gohome/internal/tui/testdata/
git commit -m "feat(tui): merge consecutive thinking blocks and always show content inline"
```

---

### Task 4: Expand Hint on Truncated Tool Output

**Files:**
- Modify: `gohome/internal/tui/chat.go:441-449` (non-expanded tool branch, after preview lines)
- Modify: `gohome/internal/tui/chat.go:411-416` (shadow non-expanded branch)
- Modify: `gohome/internal/tui/chat.go:173-181` (`entryLineCount` tool non-expanded preview)
- Modify: `gohome/internal/tui/chat.go:251-260` (`countLines` tool non-expanded preview)
- Test: `gohome/internal/tui/tui_snapshot_test.go`
- Golden: `gohome/internal/tui/testdata/TestSnapshots/tool_output_preview.golden`

**Step 1: Write a test for the expand hint**

```go
func TestExpandHintOnTruncatedOutput(t *testing.T) {
	// 10-line result, only 3 shown in preview.
	result := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10"
	timeline := []TimelineEntry{
		{
			Kind:       KindTool,
			ToolName:   "bash",
			Text:       `{"command":"find ."}`,
			ToolResult: result,
			Status:     "success",
		},
	}
	chat := NewChat(&timeline, 24)
	lines := chat.Render(80)

	// Should contain the hint with correct count: 10 - 3 = 7 earlier lines.
	found := false
	for _, l := range lines {
		if strings.Contains(l, "7 earlier lines") && strings.Contains(l, "enter to expand") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected expand hint '7 earlier lines, enter to expand', got lines: %v", lines)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/tui/ -run TestExpandHintOnTruncatedOutput -v`
Expected: FAIL -- no expand hint in output.

**Step 3: Implement the expand hint**

In the `renderEntry` `KindTool` non-expanded branch for regular entries (~line 442-449), after the preview lines loop, add:

```go
if !e.Expanded {
	if pv := previewLines(e.ToolResult, maxPreviewLines); len(pv) > 0 {
		for _, pl := range pv {
			for _, wl := range WrapText(pl, maxWidth-9) {
				lines = append(lines, "       "+ansiDim+wl+ansiReset)
			}
		}
		// Expand hint
		totalLines := len(strings.Split(strings.TrimSpace(e.ToolResult), "\n"))
		if totalLines > maxPreviewLines {
			hidden := totalLines - maxPreviewLines
			hint := fmt.Sprintf("... (%d earlier lines, enter to expand)", hidden)
			lines = append(lines, "       "+ansiDim+hint+ansiReset)
		}
	}
}
```

Do the same for the shadow non-expanded branch (~line 412-416), with indent `"             "`:

```go
if pv := previewLines(e.ToolResult, maxPreviewLines); len(pv) > 0 {
	for _, pl := range pv {
		for _, wl := range WrapText(pl, maxWidth-15) {
			lines = append(lines, "             "+ansiDim+wl+ansiReset)
		}
	}
	totalLines := len(strings.Split(strings.TrimSpace(e.ToolResult), "\n"))
	if totalLines > maxPreviewLines {
		hidden := totalLines - maxPreviewLines
		hint := fmt.Sprintf("... (%d earlier lines, enter to expand)", hidden)
		lines = append(lines, "             "+ansiDim+hint+ansiReset)
	}
}
```

Update `entryLineCount` and `countLines` to account for the extra hint line. In the preview section, after the preview line count, add:

```go
totalLines := len(strings.Split(strings.TrimSpace(e.ToolResult), "\n"))
if totalLines > maxPreviewLines {
	count++ // hint line
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/tui/ -run TestExpandHintOnTruncatedOutput -v`
Expected: PASS

**Step 5: Regenerate golden files and run full suite**

Run: `go test ./gohome/internal/tui/ -run TestSnapshots -update && go test ./gohome/internal/tui/ -v`
Expected: all PASS.

**Step 6: Commit**

```bash
git add gohome/internal/tui/chat.go gohome/internal/tui/chat_test.go gohome/internal/tui/testdata/
git commit -m "feat(tui): show expand hint with line count on truncated tool output"
```

---

### Task 5: Final Verification

**Files:** None new -- verification only.

**Step 1: Run lint**

Run: `golangci-lint run ./gohome/...`
Expected: no errors.

**Step 2: Run vet**

Run: `go vet ./gohome/...`
Expected: no errors.

**Step 3: Run full test suite**

Run: `go test ./gohome/...`
Expected: all PASS.

**Step 4: Build**

Run: `go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome`
Expected: binary built successfully.

**Step 5: Manual smoke test**

Run: `./bin/gohome --model <available-model>`
Verify:
- Tool calls display as `$ command`, `filepath`, etc.
- User messages appear in full-width background blocks.
- Thinking text appears inline without repeated "Thinking..." lines.
- Truncated tool output shows "... (N earlier lines, enter to expand)".
- Enter key still expands tool entries.
