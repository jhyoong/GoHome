# Uniform Entry Spacing Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace group-based separator logic with uniform blank-line spacing between every timeline entry.

**Architecture:** Delete the `entryGroup()` helper and replace three loops in `chat.go` that use group-transition logic with a simpler "has any entry rendered yet?" boolean. A blank line is inserted before every entry that produces output, except the first.

**Tech Stack:** Go, Bubble Tea TUI, charmbracelet/x/exp/golden for snapshot tests.

---

### Task 1: Update separator tests to expect uniform spacing

The existing tests assert group-based behavior. Update them first so they define the new expected behavior before changing the implementation.

**Files:**
- Modify: `gohome/internal/tui/chat_test.go:655-731`

**Step 1: Rewrite `TestCountLinesIncludesSeparators`**

The test has 3 entries (user, assistant, user). With uniform spacing, every entry after the first gets a separator. That's 2 separators, same count (5). Update the comment to reflect the new logic:

```go
func TestCountLinesIncludesSeparators(t *testing.T) {
	entries := []TimelineEntry{
		{Kind: KindUser, Text: "hello"},
		{Kind: KindAssistant, Text: "world"},
		{Kind: KindUser, Text: "again"},
	}
	c := NewChat(&entries, 40)
	// Each entry = 1 content line.
	// 2 separators (before entries 1 and 2) = 2 blank lines.
	// Total = 3 + 2 = 5.
	got := c.countLines(80)
	if got != 5 {
		t.Errorf("countLines = %d, want 5 (3 entries + 2 separators)", got)
	}
}
```

**Step 2: Rewrite `TestEnsureCursorVisibleAccountsForSeparators`**

With uniform spacing, every entry after the first gets a separator. 5 user entries + 1 assistant entry = 5 separators total (before entries 1-5). The cursor at index 5 has cursorTop = 5 content lines + 5 separators = 10. scrollTop = 10 + 1 - 4 = 7.

```go
func TestEnsureCursorVisibleAccountsForSeparators(t *testing.T) {
	// 5 user entries, then 1 assistant entry at index 5.
	// Each entry after the first gets a separator blank line.
	// cursorTop for entry 5 = 5 content lines + 5 separators = 10.
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

	// scrollTop = 10 + 1 - 4 = 7.
	if c.scrollTop != 7 {
		t.Errorf("scrollTop = %d, want 7 (accounting for separators)", c.scrollTop)
	}
}
```

**Step 3: Rewrite `TestRenderInsertsSeparatorBetweenTurns`**

Rename to `TestRenderInsertsSeparatorBetweenEntries`. Same 3 entries, same 5 lines, same blank lines at indices 1 and 3. Update comments:

```go
func TestRenderInsertsSeparatorBetweenEntries(t *testing.T) {
	entries := []TimelineEntry{
		{Kind: KindUser, Text: "hello"},
		{Kind: KindAssistant, Text: "reply"},
		{Kind: KindUser, Text: "follow-up"},
	}
	c := NewChat(&entries, 40)
	lines := c.Render(80)
	// 3 content lines + 2 separators = 5 total.
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5 (3 content + 2 separators)", len(lines))
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

**Step 4: Rewrite `TestNoSeparatorAroundNotice`**

With uniform spacing, notice entries also get separators. 3 entries = 2 separators = 5 total lines:

```go
func TestNoSeparatorAroundNotice(t *testing.T) {
	entries := []TimelineEntry{
		{Kind: KindUser, Text: "hello"},
		{Kind: KindNotice, Text: "reconnected"},
		{Kind: KindAssistant, Text: "reply"},
	}
	c := NewChat(&entries, 40)
	lines := c.Render(80)
	// 3 entries + 2 separators = 5 total.
	if len(lines) != 5 {
		t.Errorf("got %d lines, want 5", len(lines))
	}
}
```

**Step 5: Add a new test for same-kind separator**

This tests the key behavioral change -- entries of the same kind now also get separators:

```go
func TestSeparatorBetweenSameKindEntries(t *testing.T) {
	entries := []TimelineEntry{
		{Kind: KindAssistant, Text: "first"},
		{Kind: KindAssistant, Text: "second"},
		{Kind: KindAssistant, Text: "third"},
	}
	c := NewChat(&entries, 40)
	lines := c.Render(80)
	// 3 content lines + 2 separators = 5 total.
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5 (3 content + 2 separators)", len(lines))
	}
	plain1 := StripAnsi(lines[1])
	if strings.TrimSpace(plain1) != "" {
		t.Errorf("line[1] should be blank separator, got %q", plain1)
	}
	plain3 := StripAnsi(lines[3])
	if strings.TrimSpace(plain3) != "" {
		t.Errorf("line[3] should be blank separator, got %q", plain3)
	}
}
```

**Step 6: Run tests to verify they fail**

Run: `go test ./gohome/internal/tui/ -run "TestCountLinesIncludesSeparators|TestEnsureCursorVisibleAccountsForSeparators|TestRenderInsertsSeparatorBetween|TestNoSeparatorAroundNotice|TestSeparatorBetweenSameKindEntries" -v`

Expected: `TestEnsureCursorVisibleAccountsForSeparators`, `TestNoSeparatorAroundNotice`, and `TestSeparatorBetweenSameKindEntries` FAIL (the others happen to have the same numeric result).

---

### Task 2: Replace group-based separator logic with uniform spacing

**Files:**
- Modify: `gohome/internal/tui/chat.go:132-335`

**Step 1: Update `EnsureCursorVisible()` (lines 132-190)**

Replace the group-based loop with a `hasOutput` boolean. Replace lines 143-158 with:

```go
	cursorTop := 0
	hasOutput := false
	for i := 0; i < c.cursor; i++ {
		n := c.entryLineCount(&(*c.timeline)[i], maxWidth)
		if n > 0 {
			if hasOutput {
				cursorTop++ // separator
			}
			hasOutput = true
		}
		cursorTop += n
	}
	cursorHeight := c.entryLineCount(&(*c.timeline)[c.cursor], maxWidth)
	if cursorHeight > 0 && hasOutput {
		cursorTop++ // separator before cursor entry
	}
```

This replaces lines 143-159 (the `prevGroup` / `entryGroup` / `cursorGroup` block).

**Step 2: Update `countLines()` (lines 242-288)**

Replace the group-based loop with a `hasOutput` boolean. Replace lines 247-256 block with:

```go
	count := 0
	hasOutput := false
	for i := range *c.timeline {
		e := &(*c.timeline)[i]

		n := c.entryLineCount(e, maxWidth)
		if n > 0 {
			if hasOutput {
				count++ // separator blank line
			}
			hasOutput = true
		}
```

Remove the `prevGroup` variable and the `entryGroup` call. Keep the rest of the counting logic unchanged (the `switch` block that adds to `count`).

**Step 3: Update `Render()` loop (lines 323-334)**

Replace the group-transition separator with a `hasOutput` boolean. Replace lines 324-334 with:

```go
	var all []string
	hasOutput := false
	for i := range *c.timeline {
		e := &(*c.timeline)[i]

		marker := "  "
		if i == c.cursor {
			marker = "> "
		}

		if !e.cacheValid(renderWidth) {
			e.cachedLines = c.renderEntry(e, renderWidth, marker)
			e.cachedWidth = renderWidth
			e.cachedExpanded = e.Expanded
			e.cachedText = e.Text
			e.cachedResult = e.ToolResult
			e.cachedDiffStatus = e.Status
		}

		if len(e.cachedLines) > 0 {
			if hasOutput {
				all = append(all, "")
			}
			hasOutput = true
		}

		all = append(all, e.cachedLines...)
	}
```

**Step 4: Delete `entryGroup()` (lines 226-238)**

Remove the entire function:

```go
// entryGroup classifies a timeline entry kind into a role group for separator
// logic. User entries map to "user", assistant-side entries (assistant,
// thinking, tool, stats) map to "assistant", and neutral kinds return "".
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

**Step 5: Run the updated tests**

Run: `go test ./gohome/internal/tui/ -run "TestCountLinesIncludesSeparators|TestEnsureCursorVisibleAccountsForSeparators|TestRenderInsertsSeparatorBetween|TestNoSeparatorAroundNotice|TestSeparatorBetweenSameKindEntries" -v`

Expected: All PASS.

**Step 6: Commit**

```bash
git add gohome/internal/tui/chat.go gohome/internal/tui/chat_test.go
git commit -m "feat(tui): replace group-based separators with uniform entry spacing"
```

---

### Task 3: Update snapshot golden files and verify full test suite

**Files:**
- Modify: `gohome/internal/tui/testdata/TestSnapshots/*.golden` (regenerated)

**Step 1: Run full test suite to see what fails**

Run: `go test ./gohome/internal/tui/ -v 2>&1 | tail -40`

Expected: Snapshot tests fail due to changed spacing.

**Step 2: Regenerate golden files**

Run: `go test ./gohome/internal/tui/ -run TestSnapshots -update`

Expected: Golden files are updated.

**Step 3: Run full test suite again**

Run: `go test ./gohome/internal/tui/ -v`

Expected: All tests PASS.

**Step 4: Run vet and lint**

Run: `go vet ./gohome/... && golangci-lint run ./gohome/...`

Expected: Clean.

**Step 5: Commit**

```bash
git add gohome/internal/tui/testdata/
git commit -m "test(tui): regenerate golden snapshots for uniform entry spacing"
```

---

### Task 4: Verify scrollbar tests still work with new spacing

The scrollbar tests (`TestScrollbarGutterAppearsWhenOverflow`, `TestScrollbarThumbPosition*`) create many entries of the same kind. With uniform spacing, total line counts increase (each entry now has a separator). Verify these tests still pass -- they should, since they test behavior (thumb exists, thumb position) not exact line counts.

**Files:**
- Read: `gohome/internal/tui/chat_test.go` (scrollbar tests)

**Step 1: Run scrollbar tests**

Run: `go test ./gohome/internal/tui/ -run "TestScrollbar" -v`

Expected: All PASS.

**Step 2: Run all tests one final time**

Run: `go test ./gohome/...`

Expected: All PASS.
