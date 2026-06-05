# P2 Features Design: Thinking Blocks, File Search, Pending Message Queue

Three features from the TUI gap analysis P2 list. Each section covers architecture,
data flow, new types, and integration points.

---

## 1. Thinking Block Display (Full Stack)

### LLM Layer (`internal/llm/common/types.go`)

New kinds:

```go
BlockThinking BlockKind = "thinking"

EventThinkingDelta EventKind = "thinking_delta"
EventThinkingDone  EventKind = "thinking_done"
```

`StreamEvent` gets a `ThinkingDelta string` field (separate from `TextDelta`).

### Anthropic SSE Parser (`internal/llm/anthropic/sse.go`)

The Anthropic API sends thinking blocks as:

```json
{"type": "content_block_start", "content_block": {"type": "thinking"}}
{"type": "content_block_delta", "delta": {"type": "thinking_delta", "thinking": "..."}}
{"type": "content_block_stop"}
```

Track whether the current content block is a thinking block. On `content_block_start`,
check `content_block.type == "thinking"` and set a flag. While the flag is set, emit
`EventThinkingDelta` instead of `EventTextDelta`. On `content_block_stop`, emit
`EventThinkingDone` and clear the flag.

### Agent Layer (`internal/agent/events.go`)

New event kinds:

```go
EventThinkingDelta EventKind = "thinking_delta"
EventThinkingDone  EventKind = "thinking_done"
```

`Event` gets a `ThinkingDelta string` field. The agent's turn loop maps
`common.EventThinkingDelta` to `agent.EventThinkingDelta` and forwards via
`Frontend.Emit()`.

### TUI Layer

**TimelineEntry** -- `Kind = "thinking"`. Reuse the existing `Expanded` bool for
collapse/expand.

**handleAgentEvent** -- on `EventThinkingDelta`: append to last thinking entry if one
is in progress, otherwise create a new entry. On `EventThinkingDone`: no-op. On the
next `EventTokenDelta`, subsequent text goes to a normal assistant entry.

**ChatComponent rendering**:

- **Collapsed (default):** italic dim one-liner: `Thinking...` (or
  `Thinking... (N lines)` if content exists).
- **Expanded:** full thinking text rendered in dim italic via `RenderMarkdown`,
  indented 2 spaces.
- Toggle with Enter on cursor, same as tool entries.

**Spinner** -- start on `EventThinkingDelta` as well as `EventTokenDelta`. Show
"Thinking..." during thinking phase, switch to default message when text deltas begin.

---

## 2. @-Prefix Fuzzy File Search

### Trigger and Lifecycle

1. User types `@` -- search mode activates.
2. User types `@foo` -- after 20ms debounce, `fd --type f --color never "foo"` runs
   from cwd.
3. Results appear in a popup list anchored above the editor.
4. Up/Down navigates, Enter inserts the selected path (replacing `@query`), Esc
   dismisses.
5. Backspacing past `@` exits search mode.

### fd Subprocess

- Command: `fd --type f --color never <query>` from working directory.
- Fallback: `find . -type f -name "*<query>*"` if `fd` is not on `$PATH`.
- Results capped at 50 entries.
- Previous subprocess cancelled via `context.WithCancel` on each new query.

### Scoring and Ranking

| Match type | Score |
|---|---|
| Exact filename match | 0 |
| Filename starts with query | 20 |
| Substring in filename | 50 |
| Substring in full path | 70 |

Ties broken by path length (shorter first).

### New Component: `FileSearchPopup`

File: `internal/tui/filesearch.go`

```go
type FileSearchPopup struct {
    query    string
    results  []scoredResult
    selected int
    visible  bool
    cancel   context.CancelFunc
    debounce *time.Timer
}
```

- Implements `Component` (renders popup list).
- `SetQuery(q string)` -- updates query, resets debounce, triggers search.
- `Render(width int) []string` -- bordered box, up to 10 visible results, scroll
  indicator, selected item highlighted with reverse video.
- Does NOT implement `Interactive` -- key routing handled by `Model`.

### Integration with Model

New fields on `Model`:

```go
fileSearch    *FileSearchPopup
fileSearching bool
```

After forwarding keystrokes to `editor.HandleInput()`, scan backward from cursor for
`@` not preceded by whitespace. If found, activate search mode. If not found, deactivate.

When `fileSearching` is true, Up/Down/Enter/Esc intercepted before reaching editor.

### New Message Type

```go
type fileSearchResultMsg struct {
    Query   string
    Results []scoredResult
}
```

fd runs via `tea.Cmd`, sends results back. Stale results (query changed) are discarded.

### Rendering

Popup appears between chat area and editor input when `fileSearching` is true and
results are non-empty.

---

## 3. Pending Message Queue

### Queueing Behavior

When the focused session is in-flight and the user presses Enter with non-empty text,
the text is queued instead of sent. The editor clears so the user can compose another
message. Alt+Enter continues to insert a newline (unchanged).

### Model Changes

New field:

```go
pendingMessages []string
```

Plain string slice, FIFO queue.

### Submission Logic

```
if sv.InFlight:
    append text to pendingMessages
    clear editor
else:
    existing flow (timeline entry + sendInputCmd)
```

### Auto-Dequeue on Turn End

In `handleAgentEvent`, when `EventTurnDone` fires and `len(m.pendingMessages) > 0`:

1. Dequeue first message.
2. Append user timeline entry.
3. Set `sv.InFlight = true`.
4. Return `sendInputCmd(text)`.

Creates a chain until the queue drains.

### New Component: `PendingMessagesComponent`

File: `internal/tui/pending.go`

```go
type PendingMessagesComponent struct {
    messages *[]string
}
```

- Implements `Component`.
- Returns nothing if empty. Otherwise renders:
  ```
  Queued:
    [1] fix the tests
    [2] also update the README
  ```
- Messages truncated via `TruncateText`.

### Removing Queued Messages

Ctrl+D with empty editor removes the last queued message. Status message confirms:
`"Removed queued message (N remaining)"`.

### Rendering

Renders between spinner and editor in `View()`:

```
session strip
notification line
chat area
spinner
pending messages    <-- new
status message
editor / approval
status bar
```

### Edge Cases

- **Session switch:** Queue is global, not per-session. Drains into the session that
  was focused when the messages were queued.
- **Cancel:** `/cancel` clears the pending queue in addition to cancelling the
  in-flight turn.
- **Queue limit:** 10 messages max. Status message on overflow:
  `"Message queue full (10)"`.
