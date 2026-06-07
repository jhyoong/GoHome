# Session UX Improvements Design

Date: 2026-06-07

Three improvements to session history interaction: thinking content retention across resume, scroll-stable block expansion with visual highlighting, and copy-to-clipboard for timeline entries.

## Feature 1: Thinking Content Retention

### Problem

Thinking blocks are streamed to the TUI during a live session but never persisted to the JSONL session file. The `Turn()` function in `agent/turn.go` accumulates text and tool_use blocks but discards thinking content. When a session is resumed via `/resume`, thinking blocks are missing from the reconstructed timeline.

### Solution

Accumulate thinking deltas in `agent/turn.go` alongside the existing `textBuf`. When building the assistant message's `blocks` slice in the `done:` label, prepend a `BlockThinking` block if thinking content exists.

### Files changed

- `agent/turn.go` — add `thinkingBuf`, accumulate thinking deltas, include `BlockThinking` in persisted blocks

### Files unchanged (already work)

- `session/events.go` — `AssistantMessage.Content` is `[]common.Block`, supports `BlockThinking`
- `session/load.go` — deserializes `BlockThinking` blocks from JSONL
- `tui/history_convert.go` — converts `BlockThinking` into `KindThinking` timeline entries

## Feature 2: Scroll-Stable Block Expansion

### Problem

Pressing Enter to expand a tool or thinking block calls `rebuildViewport()`, which calls `ScrollToBottom()`. This jumps the viewport away from the cursor position, making it disorienting to browse history.

Additionally, expanded blocks blend into surrounding content with no visual boundary.

### Solution

1. When toggling expansion via Enter, rebuild the viewport without resetting scroll position. Either add a flag to `rebuildViewport()` or use a dedicated method that preserves `scrollTop` and `autoScroll` state.

2. In `chat.go`'s `Render()`, apply a subtle lipgloss background color (`lipgloss.Color("236")` — dark gray) to all lines of expanded thinking and tool blocks.

### Files changed

- `tui/model.go` — toggle path skips auto-scroll reset
- `tui/chat.go` — apply background style to expanded block lines

## Feature 3: Copy to Clipboard

### Problem

No way to copy timeline entry content to the system clipboard while browsing session history.

### Solution

Add a 'c' keypress in the cursor navigation mode (editor empty). When pressed, build the full text of the entry at `m.cursor` and write it to the system clipboard using `github.com/atotto/clipboard`. Show "Copied to clipboard" via `statusMsg`.

Content mapping per entry kind:
- `KindUser` — message text
- `KindAssistant` — markdown text
- `KindThinking` — full thinking text
- `KindTool` — tool name + args + result concatenated
- `KindNotice` — notice text

### Files changed

- `go.mod` / `go.sum` — add `github.com/atotto/clipboard`
- `tui/model.go` — handle 'c' keypress, build text, call `clipboard.WriteAll()`
- `tui/help.go` — add 'c' to keybinding list
