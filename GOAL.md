## 1. Mid-turn prompt steering does not work

User input typed while the agent is busy is queued into `pendingMessages` (up to 10) in `model_keys.go:130-139`, but this queue is only drained after `EventTurnDone` fires -- meaning after `Run()` has fully returned. The agent run loop in `run.go` is a tight sequential `Turn() -> dispatch tools -> Turn()` cycle with no check for new user input between tool calls. `AwaitUserInput()` exists on the `Frontend` interface but is only called from the outer REPL in `main.go:113`, never from within `Run()`. The only mid-turn user interaction is the guard approval prompt, which includes a "steer" option (`dec.SteerMessage`), but this only triggers on tool calls that require approval. The only interrupt mechanism is context cancellation (Ctrl+C/Esc), which aborts the entire run rather than injecting a steering message.

**To fix:** Either have `Run()` poll an input channel between tool dispatches, or expand the approval-steer mechanism to be user-triggerable at any point (e.g., a keybinding that injects a steering message into the current turn).

**Key files:** `agent/run.go`, `tui/model_agent.go:163-173`, `tui/model_keys.go:130-139`, `agent/events.go:64-68`

---

## 2. Mouse scrolling broke text selection for copy/paste

`tea.WithMouseCellMotion()` was added in commit `ed49ca7` to enable mouse wheel scrolling. This enables xterm SGR mode 1002 (button-event tracking), which captures all mouse clicks/drags and sends them to the application instead of letting the terminal handle native text selection. This is a fundamental property of terminal mouse tracking -- you cannot have both application-level mouse events and native text selection simultaneously.

A workaround was added in commit `52902b8`: left-click shows a 3-second "Hold Shift to select text" hint in the status bar (`model.go:405`, `statusbar.go:101`). Holding Shift bypasses application mouse tracking in most terminal emulators. The `c` keybinding (`model_keys.go:181-189`) copies the cursored timeline entry to clipboard via `atotto/clipboard` as a programmatic alternative.

**Options:** Accept the Shift workaround and make it more discoverable, toggle mouse mode off via a keybinding, or switch to `tea.WithMouseAllMotion()` only when the user is in the chat viewport area.

**Key files:** `cmd/gohome/main.go:562`, `tui/model.go:396-409`, `tui/statusbar.go:101-102`

---

## 3. Rendering slowdown with large context (40k+ tokens)

Multiple compounding bottlenecks:

1. **No viewport-aware rendering.** `ChatComponent.Render()` in `chat.go:338-426` iterates ALL timeline entries on every frame, then slices to the visible window. For 200 entries, all 200 are processed even if only 30 lines are visible.
2. **O(S) re-render per streaming delta.** The in-progress assistant entry cache is always invalidated (`cacheValid()` in `model.go:65` compares full strings). `RenderMarkdown()` in `markdown.go:39-56` does a full goldmark parse + AST walk on every frame for the growing entry. Code blocks hit `highlightCode()` (Chroma lexer) each time.
3. **Double iteration.** `countLines()` in `chat.go:283-334` iterates all entries a second time per frame (called via `statusBar() -> ScrollInfo()`). For uncached entries, this calls `RenderMarkdown()` again and discards the result.
4. **Regex in inner loops.** `WrapText()` in `ansi.go:90-269` runs regex per token for ANSI stripping and grapheme segmentation.
5. **Throttle defaults to disabled.** `renderThrottleMs` defaults to 0 in `config/skeleton.go:34`. Even when enabled, it only defers `rebuildViewport()` -- Bubble Tea still calls `View()` after every `Update()`.

**Key files:** `tui/chat.go`, `tui/markdown.go`, `tui/ansi.go`, `tui/model.go:61-68`, `tui/model_agent.go:251-277`

---

## 4. Auto-compact invalidates LLM prompt cache

Compaction in `agent/compact.go:44-139` replaces `sess.History` with a synthetic `[Auto-compact summary]` user message at index 0 followed by the last ~4 messages. This completely changes the message prefix, which breaks Anthropic's automatic prompt caching (prefix-based byte matching). The system prompt is unchanged, so it cache-hits, but all message tokens -- the majority of input tokens -- are a total cache miss on the first post-compaction turn.

Additionally, the codebase sends the system prompt as a plain string in `anthropic/request.go:70-107` with no `cache_control` fields. It relies entirely on Anthropic's automatic prefix caching, not explicit cache breakpoints. The summary text is LLM-generated and non-deterministic, so even repeated compaction at the same point would produce different prefixes.

**Possible fix:** Insert the summary as a user message appended after a stable prefix of the original conversation (keeping the first few messages intact), rather than replacing the entire history. Alternatively, implement explicit `cache_control` breakpoints on the system prompt and on a stable message prefix to guarantee partial cache reuse.

**Key files:** `agent/compact.go:119`, `agent/turn.go:26-33`, `llm/anthropic/request.go:70-107`, `llm/anthropic/translate.go:22-24`

---

## 5. Long tool approval prompt cannot be scrolled

The approval overlay in `approval.go:93` renders into a fixed, non-scrollable lipgloss bordered box placed in the "input region" slot of `View()`. The chat height is calculated assuming the input region is 5 lines (`editorMinHeight + 2`), but the approval box is typically 8-9 lines (summary + 4 menu options + border + hint). With a long tool argument (e.g., a long shell command or inline code), `approvalSummaryLine()` returns the extracted argument with no truncation -- lipgloss wraps it within the box, which can grow to many lines, pushing the menu options and status bar off screen.

PgUp/PgDown during approval scrolls the chat timeline (`model_approval.go:75-85`), but the approval content itself has no scroll state, no viewport, no height clamping. Up/Down arrows only navigate the 4 menu options, not the approval content.

**To fix:** Either truncate/collapse the tool argument display with an expand toggle, or wrap the approval content in a scrollable viewport with its own scroll state, and recalculate `chatH` based on the actual approval box height.

**Key files:** `tui/approval.go:93-138`, `tui/model_approval.go:75-99`, `tui/model.go:541-632`

---

## 6. Queued messages fire as separate turns, no queue management

When a turn ends (`EventTurnDone` in `model_agent.go:163-173`), exactly one message is popped from `pendingMessages` and sent via `sendInputCmd()`. If 3 messages were queued, the agent processes 3 sequential turns. The user likely intended them as one combined message.

The `PendingMessagesComponent` in `pending.go` renders a read-only numbered list ("Queued: [1] ... [2] ..."), but there is no UI to edit, reorder, or selectively delete items. The original design doc (`docs/old/2026-06-06-p2-features-design.md:216-217`) specified Ctrl+D to remove the last queued message, but this was never implemented. `/cancel` clears the queue but also aborts the in-flight turn -- there is no way to clear just the queue.

**To fix:**
- Combine: on `EventTurnDone`, drain all pending messages, join with newlines, send as one turn.
- Delete: implement Ctrl+D to pop the last queued message, or add cursor navigation within the queue list.
- Selective clear: add `/clear-queue` that clears pending messages without cancelling the current turn.

**Key files:** `tui/model_agent.go:163-173`, `tui/model_keys.go:130-139`, `tui/pending.go`, `tui/model.go:115`
