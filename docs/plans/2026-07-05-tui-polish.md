# TUI Polish: Turn Separators & Scrollbar Gutter — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add blank-line separators between conversation turns and a scrollbar gutter to the chat area.

**Architecture:** Both features live entirely in `ChatComponent` (`chat.go`). A shared helper `entryGroup()` classifies timeline entries into "user" or "assistant" groups for separator logic. The scrollbar gutter is a post-processing step in `Render()` that appends a 1-character column to each visible line when content overflows.

**Tech Stack:** Go, lipgloss (ANSI styling), existing `ChatComponent` rendering pipeline.

---

### Task 1: Add `entryGroup` helper and separator logic to `countLines`

**Files:**
- Modify: `gohome/internal/tui/chat.go:216-253` (`countLines`)
- Test: `gohome/internal/tui/chat_test.go`

**Step 1: Write the failing test**

Add to `chat_test.go`:

```go
func TestCountLinesIncludesSeparators(t *testing.T) {
	entries := []TimelineEntry{
		{Kind: KindUser, Text: "hello"},
		{Kind: KindAssistant, Text: "world"},
		{Kind: KindUser, Text: "again"},
	}
	c := NewChat(&entries, 40)
	// Each user/assistant entry = 1 content line.
	// Two role-group transitions (user->assistant, assistant->user) = 2 separator lines.
	// Total = 3 + 2 = 5.
	got := c.countLines(80)
	if got != 5 {
		t.Errorf("countLines = %d, want 5 (3 entries + 2 separators)", got)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/tui/ -run TestCountLinesIncludesSeparators -v`
Expected: FAIL — `countLines = 3, want 5`

**Step 3: Implement `entryGroup` helper and update `countLines`**

Add above `countLines` in `chat.go`:

```go
// entryGroup returns the role group for separator logic.
// "user" for KindUser, "assistant" for KindAssistant/KindThinking/KindTool/KindStats.
// KindNotice returns "" (neutral, never triggers a separator).
func entryGroup(kind string) string {
	switch kind {
	case KindUser:
		return "user"
	case KindAssistant, KindThinking, KindTool, KindStats:
		return "assistant"
	default:
		return ""
	}
}
```

Update `countLines` — after the existing `count := 0` line and before the `for` loop, add `prevGroup := ""`. Inside the loop, before the `switch`, add:

```go
group := entryGroup(e.Kind)
if group != "" && prevGroup != "" && group != prevGroup {
	count++ // separator blank line
}
if group != "" {
	prevGroup = group
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/tui/ -run TestCountLinesIncludesSeparators -v`
Expected: PASS

**Step 5: Commit**

```
feat(tui): add entryGroup helper and separator counting in countLines
```

---

### Task 2: Add separator logic to `EnsureCursorVisible`

**Files:**
- Modify: `gohome/internal/tui/chat.go:133-178` (`EnsureCursorVisible`)
- Test: `gohome/internal/tui/chat_test.go`

**Step 1: Write the failing test**

```go
func TestEnsureCursorVisibleAccountsForSeparators(t *testing.T) {
	// 5 user entries, then 1 assistant entry at index 5.
	// Without separators: cursorTop for entry 5 = 5 lines.
	// With separator before entry 5 (user->assistant): cursorTop = 5 + 1 = 6.
	var entries []TimelineEntry
	for i := 0; i < 5; i++ {
		entries = append(entries, TimelineEntry{Kind: KindUser, Text: "msg"})
	}
	entries = append(entries, TimelineEntry{Kind: KindAssistant, Text: "reply"})

	c := NewChat(&entries, 4) // viewport = 4 lines
	c.SetCursor(5)
	c.autoScroll = false
	c.scrollTop = 0
	c.EnsureCursorVisible(80)

	// The assistant entry is at line 6 (0-indexed), so scrollTop should
	// position it within the 4-line viewport. scrollTop = 6 + 1 - 4 = 3.
	if c.scrollTop != 3 {
		t.Errorf("scrollTop = %d, want 3 (accounting for separator)", c.scrollTop)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/tui/ -run TestEnsureCursorVisibleAccountsForSeparators -v`
Expected: FAIL — `scrollTop = 2, want 3`

**Step 3: Update `EnsureCursorVisible`**

In the `cursorTop` accumulation loop (lines 143-146), add separator tracking:

```go
cursorTop := 0
prevGroup := ""
for i := 0; i < c.cursor; i++ {
	group := entryGroup((*c.timeline)[i].Kind)
	if group != "" && prevGroup != "" && group != prevGroup {
		cursorTop++ // separator
	}
	if group != "" {
		prevGroup = group
	}
	cursorTop += c.entryLineCount(&(*c.timeline)[i], maxWidth)
}
// Check if cursor entry itself is preceded by a separator.
cursorGroup := entryGroup((*c.timeline)[c.cursor].Kind)
if cursorGroup != "" && prevGroup != "" && cursorGroup != prevGroup {
	cursorTop++
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/tui/ -run TestEnsureCursorVisibleAccountsForSeparators -v`
Expected: PASS

**Step 5: Commit**

```
feat(tui): account for separators in EnsureCursorVisible
```

---

### Task 3: Add separator lines to `Render`

**Files:**
- Modify: `gohome/internal/tui/chat.go:255-318` (`Render`)
- Test: `gohome/internal/tui/chat_test.go`

**Step 1: Write the failing test**

```go
func TestRenderInsertsSeparatorBetweenTurns(t *testing.T) {
	entries := []TimelineEntry{
		{Kind: KindUser, Text: "hello"},
		{Kind: KindAssistant, Text: "reply"},
		{Kind: KindUser, Text: "follow-up"},
	}
	c := NewChat(&entries, 40)
	lines := c.Render(80)
	// 3 content lines + 2 separators = 5 total.
	if len(lines) != 5 {
		t.Errorf("got %d lines, want 5 (3 content + 2 separators)", len(lines))
	}
	// Lines at index 1 and 3 should be blank separators.
	plain := StripAnsi(lines[1])
	if strings.TrimSpace(plain) != "" {
		t.Errorf("line[1] should be blank separator, got %q", plain)
	}
	plain3 := StripAnsi(lines[3])
	if strings.TrimSpace(plain3) != "" {
		t.Errorf("line[3] should be blank separator, got %q", plain3)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/tui/ -run TestRenderInsertsSeparatorBetweenTurns -v`
Expected: FAIL — `got 3 lines, want 5`

**Step 3: Add separator injection to `Render`**

In the `Render` method, in the loop that builds `all` (around line 276), add separator tracking:

```go
var all []string
prevGroup := ""
for i := range *c.timeline {
	e := &(*c.timeline)[i]

	// Insert separator blank line at role-group transitions.
	group := entryGroup(e.Kind)
	if group != "" && prevGroup != "" && group != prevGroup {
		all = append(all, "")
	}
	if group != "" {
		prevGroup = group
	}

	marker := "  "
	if i == c.cursor {
		marker = "> "
	}
	// ... rest of existing loop body unchanged
```

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/tui/ -run TestRenderInsertsSeparatorBetweenTurns -v`
Expected: PASS

**Step 5: Also verify no separator for notice entries**

```go
func TestNoSeparatorAroundNotice(t *testing.T) {
	entries := []TimelineEntry{
		{Kind: KindUser, Text: "hello"},
		{Kind: KindNotice, Text: "reconnected"},
		{Kind: KindAssistant, Text: "reply"},
	}
	c := NewChat(&entries, 40)
	lines := c.Render(80)
	// user(1) + notice(1) + separator(1) + assistant(1) = 4
	// Notice does NOT trigger a separator on either side.
	// The separator is between user group and assistant group.
	if len(lines) != 4 {
		t.Errorf("got %d lines, want 4", len(lines))
	}
}
```

Run: `go test ./gohome/internal/tui/ -run TestNoSeparatorAroundNotice -v`
Expected: PASS (notice is neutral, separator only fires at the user->assistant boundary)

**Step 6: Run full test suite**

Run: `go test ./gohome/internal/tui/ -v`
Expected: Some snapshot tests may need golden file updates.

**Step 7: Update golden snapshots if needed**

Run: `go test ./gohome/internal/tui/ -run TestSnapshots -update`

**Step 8: Commit**

```
feat(tui): insert blank-line separators between conversation turns
```

---

### Task 4: Implement scrollbar gutter in `Render`

**Files:**
- Modify: `gohome/internal/tui/chat.go:255-318` (`Render`)
- Test: `gohome/internal/tui/chat_test.go`

**Step 1: Write the failing test**

```go
func TestScrollbarGutterAppearsWhenOverflow(t *testing.T) {
	var entries []TimelineEntry
	for i := 0; i < 20; i++ {
		entries = append(entries, TimelineEntry{Kind: KindUser, Text: "line"})
	}
	c := NewChat(&entries, 10)
	lines := c.Render(80)
	if len(lines) != 10 {
		t.Fatalf("expected 10 visible lines, got %d", len(lines))
	}
	// At least one line should end with the thumb character "┃".
	hasThumb := false
	for _, l := range lines {
		if strings.HasSuffix(l, "┃"+ansiReset) || strings.HasSuffix(l, "┃") {
			hasThumb = true
			break
		}
	}
	if !hasThumb {
		t.Errorf("no scrollbar thumb found in output:\n%s", strings.Join(lines, "\n"))
	}
}

func TestNoScrollbarWhenContentFits(t *testing.T) {
	entries := []TimelineEntry{
		{Kind: KindUser, Text: "hello"},
		{Kind: KindAssistant, Text: "world"},
	}
	c := NewChat(&entries, 40)
	lines := c.Render(80)
	for _, l := range lines {
		if strings.Contains(l, "┃") {
			t.Errorf("scrollbar should not appear when content fits: %q", l)
		}
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/tui/ -run 'TestScrollbarGutter|TestNoScrollbar' -v`
Expected: `TestScrollbarGutterAppearsWhenOverflow` FAIL, `TestNoScrollbarWhenContentFits` PASS (no thumb present)

**Step 3: Implement scrollbar gutter**

At the end of `Render()`, after the scroll/height slicing produces the final `visible` slice (currently returned directly), add a gutter post-processing step. Restructure the return section:

```go
// Apply scroll and height constraints.
total := len(all)

// Determine if scrollbar gutter is needed.
needsGutter := total > c.maxHeight && c.maxHeight > 0

if needsGutter {
	// Re-render at reduced width to make room for gutter.
	// We need to rebuild 'all' at maxWidth-1.
	// Reset caches since width changed, then re-render.
}
```

Actually, the cleaner approach given the design: check overflow **before** rendering entries. Restructure `Render` to:

1. First, compute `totalAtReduced := c.countLines(maxWidth - 1)`.
2. If `totalAtReduced > c.maxHeight`, set `renderWidth = maxWidth - 1` and `needsGutter = true`.
3. Otherwise set `renderWidth = maxWidth` and `needsGutter = false`.
4. Render all entries at `renderWidth` (the existing loop).
5. After slicing to visible lines, if `needsGutter`, append gutter characters.

The gutter append logic:

```go
if needsGutter {
	effectiveTop := c.scrollTop
	if c.autoScroll {
		effectiveTop = total - c.maxHeight
	}
	thumbSize := c.maxHeight * c.maxHeight / total
	if thumbSize < 1 {
		thumbSize = 1
	}
	thumbStart := effectiveTop * c.maxHeight / total

	for i := range visible {
		if i >= thumbStart && i < thumbStart+thumbSize {
			visible[i] += ansiDim + "┃" + ansiReset
		} else {
			visible[i] += " "
		}
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/tui/ -run 'TestScrollbarGutter|TestNoScrollbar' -v`
Expected: PASS

**Step 5: Commit**

```
feat(tui): add scrollbar gutter when chat content overflows viewport
```

---

### Task 5: Verify scrollbar position accuracy

**Files:**
- Test: `gohome/internal/tui/chat_test.go`

**Step 1: Write the position test**

```go
func TestScrollbarThumbPosition(t *testing.T) {
	var entries []TimelineEntry
	for i := 0; i < 40; i++ {
		entries = append(entries, TimelineEntry{Kind: KindUser, Text: "line"})
	}
	c := NewChat(&entries, 10)

	// Scroll to top.
	c.autoScroll = false
	c.scrollTop = 0
	lines := c.Render(80)

	// Thumb should be at/near the top.
	// thumbStart = 0 * 10 / 40 = 0
	// thumbSize = 10 * 10 / 40 = 2 (at least 1)
	firstLine := lines[0]
	if !strings.Contains(firstLine, "┃") {
		t.Errorf("thumb should be at top when scrollTop=0, line[0]: %q", firstLine)
	}

	// Scroll to bottom.
	c.ScrollToBottom()
	lines = c.Render(80)
	lastLine := lines[len(lines)-1]
	if !strings.Contains(lastLine, "┃") {
		t.Errorf("thumb should be at bottom when auto-scroll, last line: %q", lastLine)
	}
}
```

**Step 2: Run test**

Run: `go test ./gohome/internal/tui/ -run TestScrollbarThumbPosition -v`
Expected: PASS

**Step 3: Commit**

```
test(tui): verify scrollbar thumb position at top and bottom
```

---

### Task 6: Run full test suite and update snapshots

**Files:**
- Test: `gohome/internal/tui/chat_test.go`, `gohome/internal/tui/tui_snapshot_test.go`

**Step 1: Run all TUI tests**

Run: `go test ./gohome/internal/tui/ -v`
Expected: Some snapshot tests may fail due to separator lines.

**Step 2: Update golden snapshots**

Run: `go test ./gohome/internal/tui/ -run TestSnapshots -update`

**Step 3: Review snapshot diffs**

Run: `git diff gohome/internal/tui/testdata/`
Verify: blank separator lines appear between user and assistant entries. No unexpected changes.

**Step 4: Run full test suite again**

Run: `go test ./gohome/internal/tui/ -v`
Expected: All PASS.

**Step 5: Run lint and vet**

Run: `golangci-lint run ./gohome/... && go vet ./gohome/...`
Expected: Clean.

**Step 6: Commit**

```
test(tui): update golden snapshots for turn separators and scrollbar
```

---
