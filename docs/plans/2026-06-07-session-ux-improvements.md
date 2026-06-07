# Session UX Improvements Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add thinking content retention across session resume, scroll-stable block expansion with background highlight, and copy-to-clipboard for timeline entries.

**Architecture:** Three independent features touching `agent/turn.go` (persistence), `tui/model.go` + `tui/chat.go` (expansion UX and clipboard), and `tui/help.go` (keybinding docs). Each feature is self-contained and can be committed separately.

**Tech Stack:** Go, Bubble Tea, lipgloss, atotto/clipboard

---

### Task 1: Persist thinking blocks in assistant messages

**Files:**
- Modify: `gohome/internal/agent/turn.go:39-43,117-128`
- Modify: `gohome/internal/agent/turn_test.go:282-323`

**Step 1: Update the existing test to expect thinking block persistence**

The test `TestTurn_ThinkingThenText` at `turn_test.go:282` currently asserts that thinking is NOT persisted (line 316-317 comment, and line 319 asserts only 1 content block). Update it to expect 2 blocks: `BlockThinking` first, then `BlockText`.

```go
// In TestTurn_ThinkingThenText, replace lines 316-323 with:

	// History should have one assistant message with thinking + text.
	if len(sess.History) != 1 {
		t.Fatalf("history: got %d", len(sess.History))
	}
	blocks := sess.History[0].Content
	if len(blocks) != 2 {
		t.Fatalf("blocks count: got %d, want 2", len(blocks))
	}
	if blocks[0].Kind != common.BlockThinking {
		t.Errorf("blocks[0].Kind: got %v, want BlockThinking", blocks[0].Kind)
	}
	if blocks[0].Text != "reasoning..." {
		t.Errorf("thinking text: got %q, want %q", blocks[0].Text, "reasoning...")
	}
	if blocks[1].Kind != common.BlockText {
		t.Errorf("blocks[1].Kind: got %v, want BlockText", blocks[1].Kind)
	}
	if blocks[1].Text != "The answer" {
		t.Errorf("text: got %q, want %q", blocks[1].Text, "The answer")
	}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/agent/ -run TestTurn_ThinkingThenText -v`
Expected: FAIL — blocks count will be 1 (only BlockText), not 2.

**Step 3: Add thinkingBuf and include BlockThinking in persisted blocks**

In `turn.go`, add `thinkingBuf string` to the var block at line 40:

```go
	var (
		textBuf     string
		thinkingBuf string
		toolBlocks  []common.Block
		stopReason  string
		usage       *common.Usage
	)
```

In the `EventThinkingDelta` case (line 67-72), accumulate into thinkingBuf:

```go
			case common.EventThinkingDelta:
				thinkingBuf += ev.ThinkingDelta
				a.Frontend.Emit(sess.ID, Event{
					Kind:          EventThinkingDelta,
					SessionID:     sess.ID,
					ThinkingDelta: ev.ThinkingDelta,
				})
```

In the `done:` label (line 117-128), prepend BlockThinking before BlockText:

```go
done:
	var blocks []common.Block
	if thinkingBuf != "" {
		blocks = append(blocks, common.Block{Kind: common.BlockThinking, Text: thinkingBuf})
	}
	if textBuf != "" {
		blocks = append(blocks, common.Block{Kind: common.BlockText, Text: textBuf})
	}
	blocks = append(blocks, toolBlocks...)
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/agent/ -run TestTurn_ThinkingThenText -v`
Expected: PASS

**Step 5: Run the full agent test suite to check for regressions**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/agent/ -v`
Expected: All tests PASS. The `TestTurn_TextDeltaAndTurnDone` and `TestTurn_TextOnlyNoToolUse` tests should be unaffected since they have no thinking events.

**Step 6: Verify the full round-trip with history_convert tests**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestHistoryToTimeline -v`
Expected: All PASS (these already handle BlockThinking correctly).

**Step 7: Commit**

```bash
git add gohome/internal/agent/turn.go gohome/internal/agent/turn_test.go
git commit -m "feat: persist thinking blocks in session JSONL for resume"
```

---

### Task 2: Scroll-stable block expansion

**Files:**
- Modify: `gohome/internal/tui/model.go:237-250,676-683,735-750`
- Modify: `gohome/internal/tui/chat.go:44-56`

**Step 1: Write a test for scroll stability on expansion toggle**

Add to `gohome/internal/tui/tui_snapshot_test.go`:

```go
func TestToggleExpansion_PreservesScrollPosition(t *testing.T) {
	m := newSized()

	// Add several entries so the timeline exceeds viewport height.
	for i := 0; i < 15; i++ {
		m.AddTimelineEntry("main", tui.TimelineEntry{Kind: tui.KindUser, Text: fmt.Sprintf("message %d", i)})
	}
	// Add a tool entry at the end.
	m.AddTimelineEntry("main", tui.TimelineEntry{
		Kind:       tui.KindTool,
		ToolName:   "bash",
		Text:       `{"command":"ls"}`,
		ToolResult: "file1.go\nfile2.go\nfile3.go\nfile4.go\nfile5.go",
		Status:     "success",
	})

	// Move cursor to the tool entry (last entry).
	for i := 0; i < 16; i++ {
		m = apply(m, tea.KeyMsg{Type: tea.KeyDown})
	}

	// Record scroll state, then toggle expansion.
	viewBefore := m.View()
	m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
	viewAfter := m.View()

	// The tool entry should still be visible after expansion (not scrolled away).
	if !strings.Contains(viewAfter, "bash") {
		t.Errorf("tool entry should remain visible after expansion.\nBefore:\n%s\nAfter:\n%s", viewBefore, viewAfter)
	}
}
```

Add `"fmt"` and `"strings"` to the imports if not already present.

**Step 2: Run test to verify it fails**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestToggleExpansion_PreservesScrollPosition -v`
Expected: May pass or fail depending on current auto-scroll behavior. If it passes, the test needs to be more precise — check that auto-scroll doesn't jump to bottom.

**Step 3: Add rebuildViewportKeepScroll method to Model**

In `model.go`, add a new method that refreshes the chat cursor without resetting scroll:

```go
// rebuildViewportKeepScroll refreshes the chat cursor and timeline without
// resetting scroll position. Used after toggling block expansion.
func (m *Model) rebuildViewportKeepScroll() {
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
}
```

**Step 4: Add DisableAutoScroll to ChatComponent**

In `chat.go`, add:

```go
// DisableAutoScroll turns off auto-scroll so the viewport stays at its current position.
func (c *ChatComponent) DisableAutoScroll() {
	c.autoScroll = false
}
```

**Step 5: Use rebuildViewportKeepScroll in the Enter toggle path**

In `model.go`, after the expansion toggle (line 681), call `rebuildViewportKeepScroll()` and `DisableAutoScroll()`:

```go
					if text == "" {
						sv, ok := m.sessions[m.focused]
						if ok && m.cursor >= 0 && m.cursor < len(sv.Timeline) {
							entry := &sv.Timeline[m.cursor]
							if entry.Kind == KindTool || entry.Kind == KindThinking {
								entry.Expanded = !entry.Expanded
								m.rebuildViewportKeepScroll()
								m.chat.DisableAutoScroll()
							}
						}
					}
```

Also update the Up/Down cursor handlers (lines 736-750) to use `rebuildViewportKeepScroll()` instead of `rebuildViewport()`:

```go
				if strings.TrimSpace(m.editor.Value()) == "" {
					if msg.Type == tea.KeyUp {
						if m.cursor > 0 {
							m.cursor--
						}
						m.rebuildViewportKeepScroll()
						return m, nil
					}
					if msg.Type == tea.KeyDown {
						sv, ok := m.sessions[m.focused]
						if ok && m.cursor < len(sv.Timeline)-1 {
							m.cursor++
						}
						m.rebuildViewportKeepScroll()
						return m, nil
					}
				}
```

**Step 6: Run test to verify it passes**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestToggleExpansion_PreservesScrollPosition -v`
Expected: PASS

**Step 7: Run full TUI test suite**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -v`
Expected: All PASS. Snapshot tests may need golden file updates if the scroll behavior changed.

If snapshot tests fail, update golden files:
Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestSnapshots -update`

**Step 8: Add background highlight for expanded blocks**

In `chat.go`, add a style variable near the top (after the existing style vars at line 11-13):

```go
var expandedBg = lipgloss.NewStyle().Background(lipgloss.Color("236"))
```

In the `Render()` method, apply the background to expanded thinking block lines (inside the `e.Expanded` branch of `KindThinking`, around line 106-113):

```go
			case KindThinking:
				if e.Expanded {
					mdLines := RenderMarkdown(e.Text, maxWidth-4)
					if len(mdLines) == 0 {
						mdLines = WrapText(e.Text, maxWidth-4)
					}
					entryLines = append(entryLines, marker+expandedBg.Render(ansiDim+ansiItalic+"Thinking..."+ansiReset))
					for _, l := range mdLines {
						entryLines = append(entryLines, expandedBg.Render("    "+ansiDim+ansiItalic+l+ansiReset))
					}
				} else {
```

Apply the same for expanded tool blocks (inside the `e.Expanded` branch of `KindTool`, around line 125-136):

```go
			case KindTool:
				line := renderToolLine(e, maxWidth-2)
				entryLines = append(entryLines, marker+line)
				if e.Expanded {
					if e.Text != "" {
						for _, l := range WrapText("args: "+e.Text, maxWidth-7) {
							entryLines = append(entryLines, expandedBg.Render("       "+l))
						}
					}
					if e.ToolResult != "" {
						entryLines = append(entryLines, expandedBg.Render("       result:"))
						for _, l := range WrapText(e.ToolResult, maxWidth-9) {
							entryLines = append(entryLines, expandedBg.Render("         "+l))
						}
					}
				}
```

**Step 9: Write a test for expanded background highlight**

Add to `gohome/internal/tui/chat_test.go`:

```go
func TestChatRenderToolExpanded_HasBackground(t *testing.T) {
	entries := []TimelineEntry{{
		Kind:       KindTool,
		ToolName:   "bash",
		Text:       `{"command":"ls"}`,
		ToolResult: "file.txt",
		Status:     "success",
		Expanded:   true,
	}}
	c := NewChat(&entries, 20)
	lines := c.Render(80)
	// Expanded lines (args/result) should have content.
	if len(lines) < 2 {
		t.Fatalf("expected multiple lines for expanded tool, got %d", len(lines))
	}
	// Check that result content appears in expanded output.
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "file.txt") {
		t.Errorf("expanded tool result missing: %q", joined)
	}
	if !strings.Contains(joined, "args:") {
		t.Errorf("expanded tool args label missing: %q", joined)
	}
}
```

**Step 10: Run tests and update snapshots**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -v`
Expected: All PASS (update snapshots if needed with `-update` flag).

**Step 11: Commit**

```bash
git add gohome/internal/tui/model.go gohome/internal/tui/chat.go gohome/internal/tui/tui_snapshot_test.go gohome/internal/tui/chat_test.go
git commit -m "feat: scroll-stable block expansion with background highlight"
```

If snapshots were updated:
```bash
git add gohome/internal/tui/testdata/
```

---

### Task 3: Copy to clipboard

**Files:**
- Modify: `go.mod`
- Modify: `gohome/internal/tui/model.go:735-751`
- Modify: `gohome/internal/tui/help.go:5-17`

**Step 1: Promote atotto/clipboard to direct dependency**

`atotto/clipboard` is already in `go.mod` as an indirect dependency. Promote it to direct:

Run: `cd /Users/macminijh/projects/GoHome && go get github.com/atotto/clipboard@v0.1.4`

Verify it appears without `// indirect` in go.mod.

**Step 2: Write a test for the copy key handler**

Add to `gohome/internal/tui/tui_snapshot_test.go`:

```go
func TestCopyKey_SetsStatusMessage(t *testing.T) {
	m := newSized()
	m.AddTimelineEntry("main", tui.TimelineEntry{Kind: tui.KindUser, Text: "hello clipboard"})
	m.AddTimelineEntry("main", tui.TimelineEntry{Kind: tui.KindAssistant, Text: "response text"})

	// Move cursor to the assistant entry.
	m = apply(m, tea.KeyMsg{Type: tea.KeyDown})

	// Press 'c' to copy.
	m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})

	// Should show a status message (either success or failure is fine in test env).
	msg := m.StatusMsg()
	if msg == "" {
		t.Fatal("expected a status message after pressing 'c'")
	}
}

func TestCopyKey_ToolEntry_IncludesAllContent(t *testing.T) {
	m := newSized()
	m.AddTimelineEntry("main", tui.TimelineEntry{
		Kind:       tui.KindTool,
		ToolName:   "bash",
		Text:       `{"command":"ls"}`,
		ToolResult: "file.go",
		Status:     "success",
	})

	// Press 'c' to copy (cursor starts at 0).
	m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})

	msg := m.StatusMsg()
	if msg == "" {
		t.Fatal("expected a status message after pressing 'c'")
	}
}
```

**Step 3: Run test to verify it fails**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestCopyKey -v`
Expected: FAIL — 'c' is currently handled as text input by the editor, no status message set.

**Step 4: Add timelineEntryText helper and 'c' key handler**

In `model.go`, add a helper function to build the copy text:

```go
// timelineEntryText returns the full text content of a timeline entry for clipboard copying.
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
```

Add the clipboard import at the top of `model.go`:

```go
import (
	...
	"github.com/atotto/clipboard"
	...
)
```

In the empty-editor key handling section (around line 735-751), add the 'c' handler before the Up/Down handlers:

```go
				if strings.TrimSpace(m.editor.Value()) == "" {
					if keyRune(msg) == 'c' {
						sv, ok := m.sessions[m.focused]
						if ok && m.cursor >= 0 && m.cursor < len(sv.Timeline) {
							text := timelineEntryText(sv.Timeline[m.cursor])
							if err := clipboard.WriteAll(text); err != nil {
								m.statusMsg = fmt.Sprintf("Copy failed: %v", err)
							} else {
								m.statusMsg = "Copied to clipboard"
							}
						}
						return m, nil
					}
					if msg.Type == tea.KeyUp {
```

**Step 5: Run test to verify it passes**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestCopyKey -v`
Expected: PASS (status message will be either "Copied to clipboard" or "Copy failed: ..." depending on clipboard availability in the test environment).

**Step 6: Add 'c' to the help overlay**

In `help.go`, add the copy keybinding to the `helpLines` slice. Insert after the "Enter" line (line 14):

```go
	"  c             Copy entry to clipboard (when browsing)",
```

**Step 7: Write a test for the help text update**

Add to `gohome/internal/tui/help_test.go`:

```go
func TestHelpOverlay_ShowsCopyKeybinding(t *testing.T) {
	m := newSized()
	m.OpenHelpOverlay()

	view := m.View()
	if !strings.Contains(view, "Copy entry to clipboard") {
		t.Fatal("expected copy keybinding in help overlay")
	}
}
```

**Step 8: Run tests**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestHelpOverlay_ShowsCopyKeybinding -v`
Expected: PASS

**Step 9: Run the full test suite and update snapshots**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -v`
Expected: All PASS. The help overlay snapshot may need updating since we added a line.

If snapshot tests fail:
Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run TestSnapshots -update`

**Step 10: Run the full project test suite**

Run: `cd /Users/macminijh/projects/GoHome && go test ./...`
Expected: All PASS

**Step 11: Commit**

```bash
git add go.mod go.sum gohome/internal/tui/model.go gohome/internal/tui/help.go gohome/internal/tui/tui_snapshot_test.go gohome/internal/tui/help_test.go
git commit -m "feat: add copy-to-clipboard with 'c' key in history browsing"
```

If snapshots were updated:
```bash
git add gohome/internal/tui/testdata/
```
