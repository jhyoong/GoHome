# Future Work: Auto-Compact Cache, Approval Scrolling, Render Slowdown

Investigated 2026-09-13. These are lower-priority issues to address in a future cycle.

---

## 1. Auto-Compact Cache Invalidation (TODO #3)

**Problem:** Auto-compact fires (the notice appears), but the compacted conversation
invalidates the LLM's implicit prefix cache, causing higher latency and cost on
subsequent turns.

**Root cause:** The project does not implement explicit Anthropic prompt caching.
There are no `cache_control` markers, no caching beta header, and the system prompt
is serialized as a plain string (`anthropicBody.System string`) instead of a block
array. After compaction, the summary message inserted at position 2 changes the
byte-for-byte prefix, breaking whatever implicit caching the API provides.

**Fix (two parts):**

A. Implement explicit prompt caching in the Anthropic wire adapter:
   - Change `anthropicBody.System` from `string` to `[]any` (block array with
     optional `cache_control`).
   - Add `cache_control: {"type": "ephemeral"}` breakpoints on the system prompt,
     last tool definition, and last stable-prefix message.
   - Add `Anthropic-Beta: prompt-caching-2024-07-31` header if needed.

B. Adjust compact strategy for cache efficiency:
   - Mark the compact summary message with `cache_control` so subsequent turns
     cache prefix+summary together.
   - After compaction, treat the summary as part of the stable prefix (3 messages
     instead of 2).

**Key files:**
- `gohome/internal/llm/anthropic/request.go` -- `System` type change, add cache_control
- `gohome/internal/llm/anthropic/client.go` -- beta header
- `gohome/internal/llm/common/types.go` -- add CacheControl to Block or new struct
- `gohome/internal/agent/compact.go` -- mark summary for caching
- `gohome/internal/agent/turn.go` -- mark last stable message with cache control

---

## 2. Approval Prompt Not Scrollable for Long Tool Calls (TODO #4)

**Problem:** When the LLM requests a tool call with a large input (e.g. inline
Python script), the approval prompt content can exceed the terminal height. The
expanded view renders all lines with no internal scrolling. PgUp/PgDown scroll the
timeline behind the overlay, not the approval content.

**Root cause:** The `approvalPrompt` struct has no scroll state (`contentOffset`,
`viewportHeight`). When `expandedSummary == true`, the full summary is rendered
without viewport clipping.

**Fix:**
- Add `contentOffset int` to `approvalPrompt`.
- In `renderApprovalOverlay`, when expanded, apply viewport clipping: render only
  N lines starting from `contentOffset`.
- In `handleApprovalKey`, intercept PgUp/PgDown (or Shift+Up/Down) to scroll
  `contentOffset` when the summary is expanded, instead of scrolling the timeline.
- Alternative: embed a `bubbles/viewport` model in `approvalPrompt` for the content.

**Key files:**
- `gohome/internal/tui/approval.go` -- render function, add viewport clipping
- `gohome/internal/tui/model_approval.go` -- key handling for content scroll

---

## 3. Render Slowdown at High Context (TODO #6 / "To Test")

**Problem:** After ~40k tokens of context, the TUI rendering slows down noticeably
during streaming. A render throttle was added but the underlying issue persists.

**Root causes (three compounding issues):**

A. Render throttle defaults to OFF. `DefaultRenderThrottleMs = 16` is defined in
   `defaults.go:13` but `skeleton.go:34` defaults to `0`. `main.go:553` passes it
   through without fallback, so the throttle guard at `model_agent.go:253` is never
   active. Result: 100+ full renders/sec with fast-streaming LLMs.

B. Full markdown re-parse every frame. The streaming entry's cache is always invalid
   (text grows with each token). `entryLineCount()` calls `RenderMarkdown()` on the
   full text, then `renderEntry()` parses it again in Pass 2. Double work, O(n) in
   message length.

C. Pass 1 iterates ALL timeline entries every frame. While most are cached, the
   iteration overhead grows with conversation length.

**Fix (priority order):**

1. Apply default throttle in `main.go:553`:
   ```go
   throttleMs := settings.RenderThrottleMs
   if throttleMs <= 0 {
       throttleMs = config.DefaultRenderThrottleMs
   }
   ```

2. Cache line counts for streaming entries. Store `cachedLineCount` +
   `cachedLineCountLen` on the entry. If `len(e.Text) - cachedLineCountLen < maxWidth`,
   the line count has not changed -- return cached value without parsing.

3. Avoid double-render. Cache `entryLineCount()` result so `renderEntry()` can
   reuse it, or skip Pass 1 for the last entry and infer its offset.

4. (Nice to have) Incremental append for the streaming entry: only process new
   delta text instead of reparsing from scratch.

**Key files:**
- `gohome/cmd/gohome/main.go:553` -- throttle fallback
- `gohome/internal/tui/model_agent.go:253` -- throttle guard
- `gohome/internal/tui/chat.go:246-277` -- entryLineCount double-render
- `gohome/internal/config/defaults.go:13` -- DefaultRenderThrottleMs
- `gohome/internal/config/skeleton.go:34` -- skeleton default
