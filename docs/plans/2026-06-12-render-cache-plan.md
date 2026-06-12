# Render Cache Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Eliminate TUI rendering slowness on large conversations by caching rendered output per timeline entry, and add an optional render throttle toggle.

**Architecture:** Add cache fields to `TimelineEntry` so each entry stores its last rendered output. `Render()` and `countLines()` skip re-rendering entries whose cache is valid. A configurable `renderThrottleMs` setting gates how often `rebuildViewport()` fires during streaming.

**Tech Stack:** Go, Bubble Tea, existing Goldmark/Chroma rendering pipeline (unchanged)

---

### Task 1: Add cache fields to TimelineEntry

**Files:**
- Modify: `gohome/internal/tui/model.go:23-30`

**Step 1: Add the cache fields to the struct**

In `gohome/internal/tui/model.go`, replace lines 23-30:

```go
// TimelineEntry is a single item in a session's conversation history.
type TimelineEntry struct {
	Kind       string // KindUser | KindAssistant | KindTool | KindNotice
	Text       string
	ToolName   string
	ToolResult string
	Expanded   bool
	Status     string // "" | "pending" | "success" | "error" (tool entries only)
}
```

with:

```go
// TimelineEntry is a single item in a session's conversation history.
type TimelineEntry struct {
	Kind       string // KindUser | KindAssistant | KindTool | KindNotice
	Text       string
	ToolName   string
	ToolResult string
	Expanded   bool
	Status     string // "" | "pending" | "success" | "error" (tool entries only)

	cachedLines    []string
	cachedWidth    int
	cachedExpanded bool
	cachedText     string
	cachedResult   string
}
```

**Step 2: Add the cache validity helper method**

Add this method right after the struct definition (after line 30):

```go
// cacheValid reports whether the cached render output is still usable
// at the given terminal width.
func (e *TimelineEntry) cacheValid(width int) bool {
	return e.cachedLines != nil &&
		e.cachedWidth == width &&
		e.cachedExpanded == e.Expanded &&
		e.cachedText == e.Text &&
		e.cachedResult == e.ToolResult
}
```

**Step 3: Run tests to verify no regressions**

Run: `go test ./gohome/internal/tui/ -count=1`
Expected: All existing tests pass. The new fields are zero-valued by default, so `cacheValid()` returns false and everything re-renders as before.

**Step 4: Commit**

```bash
git add gohome/internal/tui/model.go
git commit -m "feat(tui): add render cache fields to TimelineEntry"
```

---

### Task 2: Write failing tests for render caching behavior

**Files:**
- Modify: `gohome/internal/tui/chat_test.go`

**Step 1: Write the test for cache reuse**

This test verifies that calling `Render()` twice on an unchanged timeline does not re-render entries (validated by checking the output is identical and the cache fields are populated). Append to `gohome/internal/tui/chat_test.go`:

```go
func TestChatRenderCacheReuse(t *testing.T) {
	entries := []TimelineEntry{
		{Kind: KindAssistant, Text: "# Hello\n\nSome **bold** text."},
		{Kind: KindUser, Text: "follow up"},
	}
	c := NewChat(&entries, 40)

	first := c.Render(80)
	if len(first) == 0 {
		t.Fatal("expected non-empty render")
	}

	// After first render, cache should be populated.
	if entries[0].cachedLines == nil {
		t.Error("expected cachedLines to be populated after first render")
	}
	if entries[0].cachedWidth != 80 {
		t.Errorf("cachedWidth: got %d, want 80", entries[0].cachedWidth)
	}

	// Second render with same state should produce identical output.
	second := c.Render(80)
	if len(first) != len(second) {
		t.Fatalf("line count mismatch: first=%d second=%d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("line %d differs:\n  first:  %q\n  second: %q", i, first[i], second[i])
		}
	}
}
```

**Step 2: Write the test for cache invalidation on width change**

```go
func TestChatRenderCacheInvalidatesOnWidthChange(t *testing.T) {
	entries := []TimelineEntry{
		{Kind: KindAssistant, Text: "Some text that will wrap differently at different widths."},
	}
	c := NewChat(&entries, 40)

	first := c.Render(80)
	cachedWidth80 := entries[0].cachedWidth

	second := c.Render(40)
	cachedWidth40 := entries[0].cachedWidth

	if cachedWidth80 != 80 {
		t.Errorf("expected cachedWidth 80 after first render, got %d", cachedWidth80)
	}
	if cachedWidth40 != 40 {
		t.Errorf("expected cachedWidth 40 after second render, got %d", cachedWidth40)
	}

	// The outputs should differ because wrapping changed.
	joined1 := strings.Join(first, "\n")
	joined2 := strings.Join(second, "\n")
	if joined1 == joined2 {
		t.Error("expected different output at different widths")
	}
}
```

**Step 3: Write the test for cache invalidation on text change**

```go
func TestChatRenderCacheInvalidatesOnTextChange(t *testing.T) {
	entries := []TimelineEntry{
		{Kind: KindAssistant, Text: "first version"},
	}
	c := NewChat(&entries, 40)
	c.Render(80)

	if entries[0].cachedText != "first version" {
		t.Errorf("cachedText: got %q, want %q", entries[0].cachedText, "first version")
	}

	// Mutate the text (simulating a token delta append).
	entries[0].Text = "first version, extended"
	c.Render(80)

	if entries[0].cachedText != "first version, extended" {
		t.Errorf("cachedText after mutation: got %q, want %q", entries[0].cachedText, "first version, extended")
	}
}
```

**Step 4: Run tests to verify they fail**

Run: `go test ./gohome/internal/tui/ -run TestChatRenderCache -v`
Expected: FAIL -- `cachedLines` is nil, `cachedWidth` is 0, etc. because `Render()` does not populate cache yet.

**Step 5: Commit the failing tests**

```bash
git add gohome/internal/tui/chat_test.go
git commit -m "test(tui): add failing tests for render cache behavior"
```

---

### Task 3: Implement render caching in Render()

**Files:**
- Modify: `gohome/internal/tui/chat.go:133-240`

**Step 1: Refactor Render() to use caching**

Replace the `Render` method (lines 133-240 of `gohome/internal/tui/chat.go`) with the cached version. The key change: for each entry, check `cacheValid(maxWidth)`. If valid, use `cachedLines`. Otherwise, render and store the result.

The entry rendering logic itself is extracted into a helper `renderEntry` to keep `Render()` clean. The rendering code inside the switch is identical to the current code -- just moved into the helper.

Replace lines 133-240 with:

```go
// Render converts the current timeline to a slice of display lines, applying
// scroll and height constraints. maxWidth is the terminal column width.
func (c *ChatComponent) Render(maxWidth int) []string {
	if c.timeline == nil || len(*c.timeline) == 0 {
		return nil
	}

	// Render all entries into lines, using cache when valid.
	var all []string
	for i := range *c.timeline {
		e := &(*c.timeline)[i]
		marker := "  "
		if i == c.cursor {
			marker = "> "
		}

		if !e.cacheValid(maxWidth) {
			e.cachedLines = c.renderEntry(e, maxWidth, marker)
			e.cachedWidth = maxWidth
			e.cachedExpanded = e.Expanded
			e.cachedText = e.Text
			e.cachedResult = e.ToolResult
		} else if i == c.cursor || (c.cursor < 0 && marker == "  ") {
			// Cache is valid but cursor marker may have changed.
			// Check if the first line's marker prefix matches.
			if len(e.cachedLines) > 0 {
				first := e.cachedLines[0]
				if len(first) >= 2 && first[:2] != marker {
					e.cachedLines = c.renderEntry(e, maxWidth, marker)
				}
			}
		}

		all = append(all, e.cachedLines...)
	}

	// Apply scroll and height constraints.
	total := len(all)
	if c.maxHeight <= 0 || total <= c.maxHeight {
		return all
	}

	if c.autoScroll {
		return all[total-c.maxHeight:]
	}

	maxScroll := total - c.maxHeight
	if c.scrollTop > maxScroll {
		c.scrollTop = maxScroll
	}
	if c.scrollTop < 0 {
		c.scrollTop = 0
	}

	end := c.scrollTop + c.maxHeight
	if end > total {
		end = total
	}
	return all[c.scrollTop:end]
}

// renderEntry produces the display lines for a single timeline entry.
func (c *ChatComponent) renderEntry(e *TimelineEntry, maxWidth int, marker string) []string {
	var lines []string

	switch e.Kind {
	case KindUser:
		prefix := userPrefix.Render("you:")
		text := WrapText(e.Text, maxWidth-len("you: ")-2)
		for j, l := range text {
			if j == 0 {
				lines = append(lines, marker+prefix+" "+l)
			} else {
				lines = append(lines, "      "+l)
			}
		}

	case KindAssistant:
		mdLines := RenderMarkdown(e.Text, maxWidth-2)
		if len(mdLines) == 0 {
			mdLines = WrapText(e.Text, maxWidth-2)
		}
		for j, l := range mdLines {
			if j == 0 {
				lines = append(lines, marker+l)
			} else {
				lines = append(lines, "  "+l)
			}
		}

	case KindThinking:
		if e.Expanded {
			mdLines := RenderMarkdown(e.Text, maxWidth-4)
			if len(mdLines) == 0 {
				mdLines = WrapText(e.Text, maxWidth-4)
			}
			lines = append(lines, marker+expandedBg.Render(ansiDim+ansiItalic+"Thinking..."+ansiReset))
			for _, l := range mdLines {
				lines = append(lines, expandedBg.Render("    "+ansiDim+ansiItalic+l+ansiReset))
			}
		} else {
			label := "Thinking..."
			if n := strings.Count(strings.TrimSpace(e.Text), "\n"); n > 0 {
				label = fmt.Sprintf("Thinking... (%d lines)", n+1)
			}
			lines = append(lines, marker+ansiDim+ansiItalic+label+ansiReset)
		}

	case KindTool:
		line := renderToolLine(*e, maxWidth-2)
		lines = append(lines, marker+line)
		if e.Expanded {
			if e.Text != "" {
				for _, l := range WrapText("args: "+e.Text, maxWidth-7) {
					lines = append(lines, expandedBg.Render("       "+l))
				}
			}
			if e.ToolResult != "" {
				lines = append(lines, expandedBg.Render("       result:"))
				for _, l := range WrapText(e.ToolResult, maxWidth-9) {
					lines = append(lines, expandedBg.Render("         "+l))
				}
			}
		}

	case KindNotice:
		line := noticeStyle.Render(fmt.Sprintf("[notice] %s", e.Text))
		lines = append(lines, marker+line)
	}

	return lines
}
```

**Step 2: Run cache tests to verify they pass**

Run: `go test ./gohome/internal/tui/ -run TestChatRenderCache -v`
Expected: PASS

**Step 3: Run all TUI tests to verify no regressions**

Run: `go test ./gohome/internal/tui/ -count=1`
Expected: All tests pass.

**Step 4: Commit**

```bash
git add gohome/internal/tui/chat.go
git commit -m "feat(tui): implement entry-level render caching in Render()"
```

---

### Task 4: Cache countLines() using cached render output

**Files:**
- Modify: `gohome/internal/tui/chat.go:86-129`

**Step 1: Write a failing test for countLines caching**

Append to `gohome/internal/tui/chat_test.go`:

```go
func TestCountLinesCacheBehavior(t *testing.T) {
	entries := []TimelineEntry{
		{Kind: KindAssistant, Text: "# Hello\n\nParagraph one."},
		{Kind: KindUser, Text: "reply"},
	}
	c := NewChat(&entries, 40)

	// Call Render first to populate caches.
	c.Render(80)

	// Now DisableAutoScroll calls countLines internally.
	// It should use cached line counts rather than re-rendering.
	c.ScrollToBottom()
	c.DisableAutoScroll(80)

	// After disabling, autoScroll should be false and scrollTop should be set.
	if c.IsAutoScroll() {
		t.Error("expected autoScroll to be false after DisableAutoScroll")
	}
}
```

**Step 2: Add the IsAutoScroll() accessor**

In `gohome/internal/tui/chat.go`, add after the `ScrollToBottom` method (after line 62):

```go
// IsAutoScroll reports whether auto-scroll is active.
func (c *ChatComponent) IsAutoScroll() bool { return c.autoScroll }
```

**Step 3: Refactor countLines to use cache**

Replace the `countLines` method (lines 86-129 of `gohome/internal/tui/chat.go`) with:

```go
// countLines returns the total number of rendered lines for all timeline entries
// at the given maxWidth. Uses cached line counts when available.
func (c *ChatComponent) countLines(maxWidth int) int {
	if c.timeline == nil {
		return 0
	}
	count := 0
	for i := range *c.timeline {
		e := &(*c.timeline)[i]
		if e.cacheValid(maxWidth) {
			count += len(e.cachedLines)
			continue
		}
		switch e.Kind {
		case KindUser:
			count += len(WrapText(e.Text, maxWidth-len("you: ")-2))
		case KindAssistant:
			lines := RenderMarkdown(e.Text, maxWidth-2)
			if len(lines) == 0 {
				lines = WrapText(e.Text, maxWidth-2)
			}
			count += len(lines)
		case KindThinking:
			if e.Expanded {
				lines := RenderMarkdown(e.Text, maxWidth-4)
				if len(lines) == 0 {
					lines = WrapText(e.Text, maxWidth-4)
				}
				count += 1 + len(lines)
			} else {
				count++
			}
		case KindTool:
			count++
			if e.Expanded {
				if e.Text != "" {
					count += len(WrapText("args: "+e.Text, maxWidth-7))
				}
				if e.ToolResult != "" {
					count++
					count += len(WrapText(e.ToolResult, maxWidth-9))
				}
			}
		case KindNotice:
			count++
		}
	}
	return count
}
```

**Step 4: Run tests**

Run: `go test ./gohome/internal/tui/ -count=1`
Expected: All tests pass.

**Step 5: Commit**

```bash
git add gohome/internal/tui/chat.go gohome/internal/tui/chat_test.go
git commit -m "feat(tui): use render cache in countLines()"
```

---

### Task 5: Handle cursor marker changes without full re-render

The cursor marker (`"> "` vs `"  "`) is baked into cached lines. When the user moves the cursor, two entries change markers. We need to invalidate those entries' caches.

**Files:**
- Modify: `gohome/internal/tui/chat.go`

**Step 1: Add lastCursor field to ChatComponent**

In the `ChatComponent` struct (lines 17-23 of `chat.go`), add a `lastCursor` field:

```go
type ChatComponent struct {
	timeline   *[]TimelineEntry
	scrollTop  int
	maxHeight  int
	autoScroll bool
	cursor     int
	lastCursor int
}
```

**Step 2: Simplify the cursor handling in Render()**

Replace the cursor-related cache invalidation block in the `Render()` method. Instead of the inline cursor check we wrote in Task 3, update `Render()` to invalidate caches for old and new cursor entries when the cursor moves. At the beginning of `Render()`, after the nil check, add:

```go
	// Invalidate cache for entries whose cursor marker changed.
	if c.lastCursor != c.cursor && c.timeline != nil {
		tl := *c.timeline
		if c.lastCursor >= 0 && c.lastCursor < len(tl) {
			tl[c.lastCursor].cachedLines = nil
		}
		if c.cursor >= 0 && c.cursor < len(tl) {
			tl[c.cursor].cachedLines = nil
		}
		c.lastCursor = c.cursor
	}
```

And simplify the cache check in the main loop to remove the cursor marker workaround from Task 3. The loop body becomes:

```go
	for i := range *c.timeline {
		e := &(*c.timeline)[i]
		marker := "  "
		if i == c.cursor {
			marker = "> "
		}

		if !e.cacheValid(maxWidth) {
			e.cachedLines = c.renderEntry(e, maxWidth, marker)
			e.cachedWidth = maxWidth
			e.cachedExpanded = e.Expanded
			e.cachedText = e.Text
			e.cachedResult = e.ToolResult
		}

		all = append(all, e.cachedLines...)
	}
```

**Step 3: Initialize lastCursor in NewChat**

In `NewChat` (line 27), set `lastCursor: -1`:

```go
func NewChat(timeline *[]TimelineEntry, maxHeight int) *ChatComponent {
	return &ChatComponent{
		timeline:   timeline,
		maxHeight:  maxHeight,
		autoScroll: true,
		cursor:     -1,
		lastCursor: -1,
	}
}
```

**Step 4: Run all tests**

Run: `go test ./gohome/internal/tui/ -count=1`
Expected: All tests pass.

**Step 5: Commit**

```bash
git add gohome/internal/tui/chat.go
git commit -m "feat(tui): invalidate cache on cursor move instead of per-render check"
```

---

### Task 6: Add RenderThrottleMs to config

**Files:**
- Modify: `gohome/internal/config/config.go:36-45`
- Modify: `gohome/internal/config/config.go:72-113` (merge logic)
- Modify: `gohome/internal/config/defaults.go`
- Modify: `gohome/internal/config/config_test.go`

**Step 1: Write a failing test for the new config field**

Append to `gohome/internal/config/config_test.go`:

```go
func TestLoad_RenderThrottleMsMerge(t *testing.T) {
	dir := t.TempDir()

	global := Settings{RenderThrottleMs: 50}
	project := Settings{RenderThrottleMs: 100}

	gPath := writeJSON(t, dir, "global.json", global)
	pPath := writeJSON(t, dir, "project.json", project)

	merged, err := Load(gPath, pPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if merged.RenderThrottleMs != 100 {
		t.Errorf("RenderThrottleMs: got %d, want 100", merged.RenderThrottleMs)
	}
}

func TestLoad_RenderThrottleMsZeroPreservesGlobal(t *testing.T) {
	dir := t.TempDir()

	global := Settings{RenderThrottleMs: 50}
	project := Settings{}

	gPath := writeJSON(t, dir, "global.json", global)
	pPath := writeJSON(t, dir, "project.json", project)

	merged, err := Load(gPath, pPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if merged.RenderThrottleMs != 50 {
		t.Errorf("RenderThrottleMs: got %d, want 50 (should preserve global)", merged.RenderThrottleMs)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/config/ -run TestLoad_RenderThrottle -v`
Expected: FAIL -- `RenderThrottleMs` field does not exist.

**Step 3: Add the field to Settings**

In `gohome/internal/config/config.go`, add to the `Settings` struct (after line 44, the `RetryBackoffMs` field):

```go
RenderThrottleMs int `json:"renderThrottleMs,omitempty"`
```

**Step 4: Add the default constant**

In `gohome/internal/config/defaults.go`, add:

```go
DefaultRenderThrottleMs = 0
```

**Step 5: Add merge logic**

In `gohome/internal/config/config.go`, in the `Load` function, add to the `merged` struct initializer (after the `RetryBackoffMs` line):

```go
RenderThrottleMs: global.RenderThrottleMs,
```

And add the project override (after the `RetryBackoffMs` override block, around line 112):

```go
if project.RenderThrottleMs != 0 {
	merged.RenderThrottleMs = project.RenderThrottleMs
}
```

**Step 6: Run tests**

Run: `go test ./gohome/internal/config/ -count=1`
Expected: All tests pass including the new ones.

**Step 7: Commit**

```bash
git add gohome/internal/config/config.go gohome/internal/config/defaults.go gohome/internal/config/config_test.go
git commit -m "feat(config): add renderThrottleMs setting"
```

---

### Task 7: Implement render throttle in the TUI

**Files:**
- Modify: `gohome/internal/tui/model.go` (add throttle fields to Model, plumb config)
- Modify: `gohome/internal/tui/model_agent.go` (throttle rebuildViewport calls)

**Step 1: Add throttle state fields to Model**

In `gohome/internal/tui/model.go`, add these fields to the `Model` struct (after line 123, the `slashCB` field):

```go
	renderThrottleMs int
	lastRenderTime   time.Time
	renderPending    bool
```

**Step 2: Add a renderThrottleMsg type and command**

Add after the `Model` struct (e.g., after line 127):

```go
// renderThrottleMsg fires when a deferred render is due.
type renderThrottleMsg struct{}
```

**Step 3: Plumb the config value into Model**

In `gohome/internal/tui/model.go`, add a setter (near the other setters around line 191):

```go
func (m *Model) SetRenderThrottleMs(ms int) { m.renderThrottleMs = ms }
```

**Step 4: Handle renderThrottleMsg in Update()**

In the `Update` method (line 287), add a case in the switch before the `default` / closing brace:

```go
	case renderThrottleMsg:
		if m.renderPending {
			m.renderPending = false
			m.lastRenderTime = time.Now()
			m.rebuildViewport()
		}
```

**Step 5: Implement throttle logic in handleAgentEvent**

In `gohome/internal/tui/model_agent.go`, replace lines 156-158:

```go
	if msg.SessionID == m.focused {
		m.rebuildViewport()
	}
```

with:

```go
	if msg.SessionID == m.focused {
		if m.renderThrottleMs > 0 &&
			(ev.Kind == agent.EventTokenDelta || ev.Kind == agent.EventThinkingDelta) {
			elapsed := time.Since(m.lastRenderTime)
			threshold := time.Duration(m.renderThrottleMs) * time.Millisecond
			if elapsed < threshold {
				if !m.renderPending {
					m.renderPending = true
					remaining := threshold - elapsed
					return tea.Tick(remaining, func(time.Time) tea.Msg {
						return renderThrottleMsg{}
					})
				}
				// Already have a pending tick scheduled -- skip this delta.
				if dequeuedCmd != nil {
					return dequeuedCmd
				}
				if m.spinner.Active() {
					return SpinnerTickCmd()
				}
				return nil
			}
			m.lastRenderTime = time.Now()
		}
		m.rebuildViewport()
	}
```

Note: the early returns bypass the spinner tick logic at the bottom of the function. We need to restructure the return flow. Move the existing return logic (lines 160-166) into the throttle-aware block. The full replacement of lines 156-167:

```go
	if msg.SessionID == m.focused {
		if m.renderThrottleMs > 0 &&
			(ev.Kind == agent.EventTokenDelta || ev.Kind == agent.EventThinkingDelta) {
			elapsed := time.Since(m.lastRenderTime)
			threshold := time.Duration(m.renderThrottleMs) * time.Millisecond
			if elapsed < threshold {
				if !m.renderPending {
					m.renderPending = true
					remaining := threshold - elapsed
					cmd := tea.Tick(remaining, func(time.Time) tea.Msg {
						return renderThrottleMsg{}
					})
					if dequeuedCmd != nil {
						return tea.Batch(dequeuedCmd, cmd)
					}
					return cmd
				}
				if dequeuedCmd != nil {
					return dequeuedCmd
				}
				if m.spinner.Active() {
					return SpinnerTickCmd()
				}
				return nil
			}
			m.lastRenderTime = time.Now()
		}
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
```

**Step 6: Run all tests**

Run: `go test ./gohome/internal/tui/ -count=1`
Expected: All tests pass. With default throttle of 0, behavior is identical to current.

**Step 7: Commit**

```bash
git add gohome/internal/tui/model.go gohome/internal/tui/model_agent.go
git commit -m "feat(tui): implement render throttle toggle"
```

---

### Task 8: Write throttle integration test

**Files:**
- Modify: `gohome/internal/tui/chat_test.go` or a new section in `gohome/internal/tui/tui_test.go`

**Step 1: Write a test that verifies throttling skips intermediate rebuilds**

Append to `gohome/internal/tui/tui_test.go` (this file is in `package tui` so has access to internals):

```go
func TestRenderThrottle_SkipsIntermediateRebuilds(t *testing.T) {
	m := New(nil, "main")
	m.SetRenderThrottleMs(100)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Send first token delta -- should render immediately.
	m, cmd1 := m.Update(agentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventTokenDelta,
		SessionID: "main",
		TextDelta: "Hello ",
	}})

	// cmd1 should be a SpinnerTickCmd (normal flow, no throttle tick needed for first render).
	_ = cmd1

	// Send second token delta immediately -- should be throttled.
	m, cmd2 := m.Update(agentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventTokenDelta,
		SessionID: "main",
		TextDelta: "world",
	}})

	// cmd2 should include a tea.Tick (the deferred render).
	if cmd2 == nil {
		t.Error("expected a command (tick) for throttled render, got nil")
	}

	// Verify the text was still appended to the timeline (content is never lost).
	sv := m.(*Model).sessions["main"]
	last := sv.Timeline[len(sv.Timeline)-1]
	if last.Text != "Hello world" {
		t.Errorf("text: got %q, want %q", last.Text, "Hello world")
	}
}
```

Note: this test references `agent.Event` -- check the import exists in the test file. If not, add:

```go
import "github.com/jhyoong/GoHome/gohome/internal/agent"
```

**Step 2: Run the test**

Run: `go test ./gohome/internal/tui/ -run TestRenderThrottle -v`
Expected: PASS

**Step 3: Commit**

```bash
git add gohome/internal/tui/tui_test.go
git commit -m "test(tui): add render throttle integration test"
```

---

### Task 9: Update snapshot golden files and run full test suite

**Files:**
- Potentially: `gohome/internal/tui/testdata/TestSnapshots/`

**Step 1: Run snapshot tests to check for drift**

Run: `go test ./gohome/internal/tui/ -run TestSnapshots -v`

If snapshots fail (the refactor changed marker handling or spacing), regenerate:

Run: `go test ./gohome/internal/tui/ -run TestSnapshots -update`

Then review the diffs to verify they are cosmetically identical or expected changes only.

**Step 2: Run the full test suite**

Run: `go test ./gohome/... -count=1`
Expected: All tests pass.

**Step 3: Run linter**

Run: `golangci-lint run ./gohome/...`
Expected: No new warnings.

**Step 4: Run vet**

Run: `go vet ./gohome/...`
Expected: Clean.

**Step 5: Build**

Run: `go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome`
Expected: Binary builds successfully.

**Step 6: Commit any snapshot updates**

```bash
git add gohome/internal/tui/testdata/
git commit -m "test(tui): update golden snapshots after render cache refactor"
```

---

### Task 10: Plumb RenderThrottleMs from main.go to TUI

**Files:**
- Modify: `gohome/cmd/gohome/main.go` (find where `SetSettings` / `SetContextWindow` etc. are called)

**Step 1: Find the wiring location**

Search `main.go` for calls like `tuiModel.SetContextWindow` or `tuiModel.SetSettings`. Add the throttle plumbing near those calls:

```go
tuiModel.SetRenderThrottleMs(settings.RenderThrottleMs)
```

**Step 2: Run the build**

Run: `go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome`
Expected: Builds cleanly.

**Step 3: Commit**

```bash
git add gohome/cmd/gohome/main.go
git commit -m "feat: plumb renderThrottleMs config to TUI model"
```
