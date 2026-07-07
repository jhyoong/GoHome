# Scroll Indicator Redesign Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove the buggy gutter scrollbar and replace it with a status bar scroll position indicator plus gradient fade on boundary lines.

**Architecture:** Delete the gutter rendering path from `ChatComponent.Render()`, add a `ScrollInfo()` method for the status bar to query scroll position, and apply `ansiDim` to boundary visible lines when content overflows.

**Tech Stack:** Go, Bubble Tea (bubbletea), lipgloss, golden snapshot tests

---

### Task 1: Remove gutter scrollbar from Render()

**Files:**
- Modify: `gohome/internal/tui/chat.go:287-398`

**Step 1: Delete the gutter width detection block**

In `Render()`, delete lines 304-313 (the `renderWidth` / `needsGutter` block). Replace with just using `maxWidth` directly. The code currently reads:

```go
	// Determine whether a scrollbar gutter is needed by checking if content
	// at reduced width overflows the viewport.
	renderWidth := maxWidth
	needsGutter := false
	if c.maxHeight > 0 {
		totalAtReduced := c.countLines(maxWidth - 1)
		if totalAtReduced > c.maxHeight {
			renderWidth = maxWidth - 1
			needsGutter = true
		}
	}
```

Delete that entire block. Then in the rest of `Render()`, replace all references to `renderWidth` with `maxWidth` (there is one on line 328: `e.cacheValid(renderWidth)` and three more on lines 329-330 assigning `e.cachedWidth = renderWidth`).

**Step 2: Delete the gutter-appending loop**

Delete lines 372-395 (the `if needsGutter` block that appends `┃` or space to each visible line):

```go
	// Append scrollbar gutter characters to visible lines.
	if needsGutter && len(visible) > 0 {
		...
	}
```

**Step 3: Run tests to verify nothing breaks**

Run: `go test ./gohome/internal/tui/ -run TestChat -v`
Expected: All `TestChat*` tests pass. The 3 gutter tests will still be present and will fail -- we delete them in the next task.

**Step 4: Commit**

```bash
git add gohome/internal/tui/chat.go
git commit -m "refactor(tui): remove gutter scrollbar rendering from ChatComponent"
```

---

### Task 2: Delete gutter scrollbar tests

**Files:**
- Modify: `gohome/internal/tui/chat_test.go:778-848`

**Step 1: Delete the three gutter test functions**

Delete these three functions entirely from `chat_test.go`:
- `TestScrollbarGutterAppearsWhenOverflow` (lines 778-800)
- `TestNoScrollbarWhenContentFits` (lines 802-817)
- `TestScrollbarThumbPosition` (lines 819-848)

**Step 2: Run all chat tests**

Run: `go test ./gohome/internal/tui/ -run TestChat -v`
Expected: All remaining tests pass.

**Step 3: Run full test suite**

Run: `go test ./gohome/internal/tui/ -v`
Expected: All tests pass including snapshots.

**Step 4: Commit**

```bash
git add gohome/internal/tui/chat_test.go
git commit -m "test(tui): remove gutter scrollbar tests"
```

---

### Task 3: Add ScrollInfo method to ChatComponent

**Files:**
- Modify: `gohome/internal/tui/chat.go`
- Test: `gohome/internal/tui/chat_test.go`

**Step 1: Write the failing test**

Add to `chat_test.go`:

```go
func TestScrollInfoReturnsPosition(t *testing.T) {
	var entries []TimelineEntry
	for i := 0; i < 20; i++ {
		entries = append(entries, TimelineEntry{Kind: KindUser, Text: "line"})
	}
	c := NewChat(&entries, 10)

	// Auto-scroll: position should be at the end.
	currentLine, totalLines := c.ScrollInfo(80)
	if totalLines <= 10 {
		t.Fatalf("totalLines = %d, want > 10", totalLines)
	}
	if currentLine != totalLines-10 {
		t.Errorf("currentLine = %d, want %d (auto-scroll at bottom)", currentLine, totalLines-10)
	}

	// Scroll to top.
	c.autoScroll = false
	c.scrollTop = 0
	currentLine, totalLines = c.ScrollInfo(80)
	if currentLine != 0 {
		t.Errorf("currentLine = %d, want 0 (scrolled to top)", currentLine)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/tui/ -run TestScrollInfoReturnsPosition -v`
Expected: FAIL with "undefined: ScrollInfo" or similar.

**Step 3: Write the implementation**

Add to `chat.go`, after the `IsAutoScroll()` method:

```go
// ScrollInfo returns the effective scroll offset and total line count at the
// given width. Used by the status bar to display scroll position.
func (c *ChatComponent) ScrollInfo(maxWidth int) (currentLine, totalLines int) {
	if c.timeline == nil || len(*c.timeline) == 0 {
		return 0, 0
	}
	totalLines = c.countLines(maxWidth)
	if totalLines <= c.maxHeight {
		return 0, totalLines
	}
	if c.autoScroll {
		currentLine = totalLines - c.maxHeight
	} else {
		currentLine = c.scrollTop
		if currentLine > totalLines-c.maxHeight {
			currentLine = totalLines - c.maxHeight
		}
		if currentLine < 0 {
			currentLine = 0
		}
	}
	return currentLine, totalLines
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/tui/ -run TestScrollInfoReturnsPosition -v`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/tui/chat.go gohome/internal/tui/chat_test.go
git commit -m "feat(tui): add ScrollInfo method to ChatComponent"
```

---

### Task 4: Add scroll position to status bar

**Files:**
- Modify: `gohome/internal/tui/statusbar.go:33-79`
- Test: `gohome/internal/tui/chat_test.go`

**Step 1: Write the failing test**

Add to `chat_test.go`:

```go
func TestStatusBarShowsScrollPosition(t *testing.T) {
	m := New("main")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	sv := m.Sessions()["main"]
	for i := 0; i < 50; i++ {
		sv.Timeline = append(sv.Timeline, TimelineEntry{Kind: KindUser, Text: "message"})
	}

	// Scroll up to disable auto-scroll.
	m.Sessions()["main"] = sv
	m.Update(tea.KeyMsg{Type: tea.KeyPgUp})

	bar := m.StatusBarForTest()
	plain := StripAnsi(bar)
	if !strings.Contains(plain, "Ln ") {
		t.Errorf("status bar should show scroll position when scrolled up, got: %q", plain)
	}
}

func TestStatusBarHidesScrollPositionAtBottom(t *testing.T) {
	m := New("main")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	sv := m.Sessions()["main"]
	for i := 0; i < 50; i++ {
		sv.Timeline = append(sv.Timeline, TimelineEntry{Kind: KindUser, Text: "message"})
	}
	m.Sessions()["main"] = sv

	bar := m.StatusBarForTest()
	plain := StripAnsi(bar)
	if strings.Contains(plain, "Ln ") {
		t.Errorf("status bar should NOT show scroll position at bottom, got: %q", plain)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/tui/ -run TestStatusBar -v`
Expected: FAIL -- `Ln ` not found in status bar output.

**Step 3: Implement scroll position in statusBar()**

In `statusbar.go`, in the `statusBar()` function, after building the main `line` string (after the yolo check at line 76), add:

```go
	if !m.chat.IsAutoScroll() {
		currentLine, totalLines := m.chat.ScrollInfo(m.winW)
		scrollPct := 0
		if totalLines > 0 {
			scrollPct = int(float64(currentLine) / float64(totalLines) * 100)
		}
		line += fmt.Sprintf(" · Ln %d/%d (%d%%)", currentLine+1, totalLines, scrollPct)
	}
```

Insert this block just before the `return m.theme.StatusBar.Render(line)` line.

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/tui/ -run TestStatusBar -v`
Expected: PASS

**Step 5: Regenerate golden snapshots**

Run: `go test ./gohome/internal/tui/ -run TestSnapshots -update`
Expected: snapshots regenerated successfully. The status bar line in snapshot files may change if any snapshot has the chat scrolled up (unlikely since snapshots are fresh models at auto-scroll).

**Step 6: Commit**

```bash
git add gohome/internal/tui/statusbar.go gohome/internal/tui/chat_test.go
git commit -m "feat(tui): show scroll position in status bar when scrolled up"
```

---

### Task 5: Add gradient fade edge hints

**Files:**
- Modify: `gohome/internal/tui/chat.go` (in `Render()`)
- Test: `gohome/internal/tui/chat_test.go`

**Step 1: Write the failing tests**

Add to `chat_test.go`:

```go
func TestGradientFadeAtTop(t *testing.T) {
	var entries []TimelineEntry
	for i := 0; i < 20; i++ {
		entries = append(entries, TimelineEntry{Kind: KindUser, Text: "line"})
	}
	c := NewChat(&entries, 10)
	c.autoScroll = false
	c.scrollTop = 5
	lines := c.Render(80)
	if len(lines) == 0 {
		t.Fatal("expected non-empty render")
	}
	// First line should have dim ANSI prefix when there is content above.
	if !strings.HasPrefix(lines[0], ansiDim) {
		t.Errorf("first line should be dimmed when content above, got: %q", lines[0])
	}
	// Last line should also be dimmed since content extends below.
	if !strings.HasPrefix(lines[len(lines)-1], ansiDim) {
		t.Errorf("last line should be dimmed when content below, got: %q", lines[len(lines)-1])
	}
}

func TestGradientFadeAtBottom(t *testing.T) {
	var entries []TimelineEntry
	for i := 0; i < 20; i++ {
		entries = append(entries, TimelineEntry{Kind: KindUser, Text: "line"})
	}
	c := NewChat(&entries, 10)
	// Auto-scroll = at the bottom. Content above exists, content below does not.
	lines := c.Render(80)
	if len(lines) == 0 {
		t.Fatal("expected non-empty render")
	}
	// First line dimmed (content above).
	if !strings.HasPrefix(lines[0], ansiDim) {
		t.Errorf("first line should be dimmed when content above, got: %q", lines[0])
	}
	// Last line NOT dimmed (at bottom, no content below).
	if strings.HasPrefix(lines[len(lines)-1], ansiDim) {
		t.Errorf("last line should NOT be dimmed at bottom, got: %q", lines[len(lines)-1])
	}
}

func TestNoGradientFadeWhenContentFits(t *testing.T) {
	entries := []TimelineEntry{
		{Kind: KindUser, Text: "hello"},
		{Kind: KindAssistant, Text: "world"},
	}
	c := NewChat(&entries, 40)
	lines := c.Render(80)
	for i, l := range lines {
		if strings.HasPrefix(l, ansiDim) {
			t.Errorf("line[%d] should NOT be dimmed when all content fits, got: %q", i, l)
		}
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/tui/ -run TestGradientFade -v`
Expected: FAIL -- lines don't have `ansiDim` prefix.

Also run: `go test ./gohome/internal/tui/ -run TestNoGradientFade -v`
Expected: PASS (no dim applied yet, so no false positives).

**Step 3: Implement gradient fade in Render()**

In `chat.go`, in `Render()`, after the scroll/height constraint block that produces `visible` (after the closing brace of the scroll logic, around line 369 after gutter removal), add:

```go
	// Apply gradient fade to boundary lines when content overflows.
	if total > c.maxHeight && len(visible) > 0 {
		effectiveTop := c.scrollTop
		if c.autoScroll {
			effectiveTop = total - c.maxHeight
		}
		if effectiveTop > 0 {
			visible[0] = ansiDim + visible[0] + ansiReset
		}
		if effectiveTop+c.maxHeight < total {
			last := len(visible) - 1
			visible[last] = ansiDim + visible[last] + ansiReset
		}
	}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/tui/ -run "TestGradientFade|TestNoGradientFade" -v`
Expected: All 3 tests PASS.

**Step 5: Run full test suite**

Run: `go test ./gohome/internal/tui/ -v`
Expected: All tests pass.

**Step 6: Regenerate golden snapshots**

Run: `go test ./gohome/internal/tui/ -run TestSnapshots -update`
Then: `go test ./gohome/internal/tui/ -run TestSnapshots -v`
Expected: Snapshots regenerated and pass. Some snapshots may now show dim ANSI on boundary lines if their content overflows the 24-line viewport.

**Step 7: Commit**

```bash
git add gohome/internal/tui/chat.go gohome/internal/tui/chat_test.go
git add gohome/internal/tui/testdata/
git commit -m "feat(tui): add gradient fade on boundary lines for scroll overflow"
```

---

### Task 6: Final verification

**Step 1: Run lint**

Run: `golangci-lint run ./gohome/...`
Expected: No lint errors.

**Step 2: Run vet**

Run: `go vet ./gohome/...`
Expected: No issues.

**Step 3: Run full test suite**

Run: `go test ./gohome/...`
Expected: All tests pass.

**Step 4: Build**

Run: `go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome`
Expected: Build succeeds.

**Step 5: Commit (if any fixups needed)**

Only commit if lint/vet required fixes. Otherwise, no commit needed.
