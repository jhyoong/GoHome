# Subagent Shadow Entries & Cursor-Viewport Sync

Date: 2026-06-25

## Overview

Two changes to the TUI:
1. Show the 3 most recent subagent tool calls inline in the parent session's timeline.
2. Sync the viewport with cursor movement so arrow keys always keep the cursor visible.

---

## Change 1: Subagent Shadow Tool Entries

### Problem

When a subagent runs, all its events go to the child's own SessionView tab. The parent session only shows a single `[tool] subagent` entry. There is no visibility into what the subagent is doing without switching tabs.

### Solution

Insert "shadow" tool entries into the parent session's timeline beneath the `[tool] subagent` entry. Keep a sliding window of the 3 most recent child tool calls. These persist after the subagent finishes.

### Data model

Add to `TimelineEntry`:
- `Shadow bool` -- marks entries as shadow copies from a child session.
- `ChildSessionID string` -- on the `[tool] subagent` entry, links to the spawned child. On shadow entries, identifies the source session.

### Event handling in handleAgentEvent

When `EventToolCallDone` or `EventToolResult` arrives for a child session (depth > 0):

1. Find the parent session. Use a `childToParent map[string]string` on Model, populated when `EventSessionStarted` fires for a depth-1 session.
2. In the parent's timeline, find the `[tool] subagent` entry with matching `ChildSessionID`. Link it on first `EventSessionStarted` by scanning backwards for the last pending subagent tool entry.
3. For `EventToolCallDone`: count shadow entries after the subagent entry. If 3 already exist, remove the oldest. Insert a new shadow entry with `Shadow: true`, `ToolName`, `Text` (InputJSON), `Status: "pending"`.
4. For `EventToolResult`: update the most recent shadow entry's `ToolResult` and `Status`.

### Rendering

Shadow entries render like normal tool entries but with 4 extra spaces of indentation and dimmed styling to visually distinguish them from parent tool calls. Reuse `renderToolLine` with a prefix.

### After completion

Shadow entries remain permanently. They serve as a summary of the subagent's last 3 actions.

### Cursor stability

When inserting/removing shadow entries, adjust `m.cursor` if the insertion/removal point is at or before the cursor index.

---

## Change 2: Cursor-Viewport Sync

### Problem

Up/Down arrow keys move `m.cursor` and call `rebuildViewportKeepScroll()`, which preserves scroll position. The cursor can move off-screen with no viewport adjustment. Page Up/Down scroll the viewport but don't move the cursor. The two are completely independent.

### Solution

Add `EnsureCursorVisible(maxWidth int)` to `ChatComponent`. Call it from the Up/Down arrow handlers after updating the cursor.

### EnsureCursorVisible logic

1. Compute the rendered line offset of the cursor entry by summing line counts of entries 0..cursor-1.
2. Compute the line count of the cursor entry itself.
3. If `cursorTop < scrollTop`: set `scrollTop = cursorTop` (scroll up, cursor at top edge).
4. If `cursorTop + cursorHeight > scrollTop + maxHeight`: set `scrollTop = cursorTop + cursorHeight - maxHeight` (scroll down, cursor at bottom edge).
5. For expanded entries spanning many lines: ensure at least the first line is visible.
6. Disable `autoScroll` when adjusting (user is actively navigating).

### Integration

- Up/Down handlers in `model_keys.go`: after `rebuildViewportKeepScroll()`, call `m.chat.EnsureCursorVisible(m.winW)`.
- Page Up/Down: unchanged. They only scroll the viewport. The cursor stays put. Next arrow key press brings the cursor into view.

### Edge cases

- Auto-scroll: `EnsureCursorVisible` does not fight with auto-scroll. It only fires during manual arrow key navigation.
- New content: `rebuildViewport()` calls `ScrollToBottom()`, keeping auto-scroll active. Cursor sync only applies during arrow key use.
- Expanded blocks: Use the first line of the entry as the anchor, not the full expanded height.

---

## Files to modify

- `gohome/internal/tui/model.go` -- add `Shadow`, `ChildSessionID` to TimelineEntry; add `childToParent` map to Model
- `gohome/internal/tui/model_agent.go` -- handle shadow entry insertion/removal on child events
- `gohome/internal/tui/chat.go` -- add `EnsureCursorVisible`; adjust `renderEntry` for shadow styling
- `gohome/internal/tui/model_keys.go` -- call `EnsureCursorVisible` from arrow handlers
- `gohome/internal/tui/tui_snapshot_test.go` -- add test cases for shadow entries and cursor sync
