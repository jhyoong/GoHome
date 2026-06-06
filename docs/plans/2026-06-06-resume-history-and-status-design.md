# Design: Fix Session Resume -- Load History & Clear Status Text

## Problems

1. When resuming a session, prior messages are not loaded into the TUI. The `session.Load()` function correctly parses the JSONL file and returns the full conversation history, but `main.go` discards it with `_`. The TUI creates a new empty `SessionView` with an empty `Timeline`.

2. The "Resumed: <session-id>" text set in `m.statusMsg` persists above the input area indefinitely. The message-send handler never clears it.

## Approach

### 1. Change `ResumeSession` callback signature

**File:** `gohome/internal/tui/slash.go`

Change `ResumeSession` from `func(id string) error` to `func(id string) ([]common.Message, error)`.

**File:** `gohome/cmd/gohome/main.go` (lines 282-312)

Stop discarding the history. Change `loaded, _, err := session.Load(path)` to `loaded, history, err := session.Load(path)` and return `history, nil`.

### 2. Convert history to Timeline entries

**File:** `gohome/internal/tui/model.go` (in the `SetOnSelect` callback, lines 932-945)

After `ResumeSession` returns successfully, iterate the returned `[]common.Message` and convert each into `TimelineEntry` objects:

- `RoleUser` message with `BlockText` blocks -> `TimelineEntry{Kind: "user", Text: <concatenated text>}`
- `RoleAssistant` message with `BlockText` blocks -> `TimelineEntry{Kind: "assistant", Text: <concatenated text>}`
- `RoleAssistant` message with `BlockThinking` blocks -> `TimelineEntry{Kind: "thinking", Text: ...}`
- `RoleAssistant` message with `BlockToolUse` blocks -> `TimelineEntry{Kind: "tool", ToolName: ..., Text: <inputJSON>, Status: "success"}`
- `RoleTool` message with `BlockToolResult` -> merged into the preceding tool entry's `ToolResult` field (same pattern as `handleAgentEvent`'s `EventToolResult` handler)

An assistant message can contain multiple blocks (thinking + text + tool_use), so each block maps to its own `TimelineEntry`. Tool results are backfilled into the most recent tool entry.

Set `m.cursor` to `len(sv.Timeline) - 1` after populating so the viewport scrolls to the bottom.

Historical and new messages flow together seamlessly -- no separator.

### 3. Clear `statusMsg` on message send

**File:** `gohome/internal/tui/model.go` (lines 641-660)

Add `m.statusMsg = ""` in the normal message-send path (after `m.editor.SetValue("")`). This clears the "Resumed: session-id" text when the user sends their first message.

### 4. Testing

- Update existing integration tests for `/resume` to verify Timeline is populated with historical entries after resume.
- Test block-to-timeline conversion with a session file containing user messages, assistant messages with thinking, tool calls, and tool results.
- Test that `statusMsg` is empty after sending a message in a resumed session.

## Files changed

| File | Change |
|------|--------|
| `gohome/internal/tui/slash.go` | Change `ResumeSession` signature to return `([]common.Message, error)` |
| `gohome/cmd/gohome/main.go` | Capture and return history from `session.Load()` |
| `gohome/internal/tui/model.go` | Convert history to Timeline entries on resume; clear `statusMsg` on send |
| Test files | Update for new callback signature and add history-loading tests |
