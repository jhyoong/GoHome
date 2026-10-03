# Performance findings

Scan of CPU and memory usage, focused on why gohome uses noticeable CPU on
lower-end machines while streaming responses.

## Summary

Text parsing and turn handling are cheap. Nearly all CPU goes to the TUI
re-rendering the full screen on every streamed token, plus a bug that
multiplies redraws.

Measured on a 4-core container: one streaming frame for a 6 KB assistant
message took ~11 ms and allocated ~3 MB / ~46k objects. At 50-100 tokens/sec
that is roughly a full core, and the cost grows with message length
(quadratic over a long response).

## High impact

### 1. Spinner tick chains multiply (bug)

`tui/model_agent.go` returns `SpinnerTickCmd()` on every `EventSending`,
`EventTokenDelta` and `EventThinkingDelta` while the spinner is active. Each
`spinnerTickMsg` handler (`tui/model.go`) schedules another tick. Every
delta therefore starts a new self-perpetuating 80 ms timer chain that only
ends when the spinner stops. After 2,000 tokens there are ~2,000 chains,
i.e. ~25,000 extra Update/View cycles per second. It also makes the spinner
animate too fast.

Fix: track whether a tick is in flight and only schedule one when none is.

### 2. Streaming entry is rendered twice per frame and never cached off-screen

In `tui/chat.go`, `entryLineCount` (pass 1 of `Render`, also used by
`countLines`, `ScrollInfo`, `EnsureCursorVisible`) fully renders any entry
whose cache is stale (markdown parse + chroma highlighting) and discards the
result. Pass 2 then renders the same entry again. Stale off-screen entries
(for example after a resize) are re-rendered on every frame because only
pass 2 writes the cache.

Fix: store the rendered lines in the cache whenever an entry is rendered.

### 3. Render throttle is ineffective

- `config.DefaultRenderThrottleMs = 16` is defined but never applied; the
  merged setting defaults to 0, so throttling is off.
- Even when set, it only defers `rebuildViewport()`. Bubble Tea calls
  `View()` after every message, so every token still causes a full render.

Fix: coalesce token/thinking deltas before they reach the Bubble Tea loop
and flush at most once per throttle interval.

### 4. Syntax highlighting recomputed from scratch

`tui/markdown.go` `highlightCode` looks up lexer, style and formatter and
re-tokenises every code block on every re-render of a message.

Fix: resolve style/formatter once and cache highlighted output keyed by
(language, code) so completed blocks are not re-highlighted while the rest
of the message streams.

### Status

Items 1-4 are fixed. `renderThrottleMs: 0` now means the 16 ms default; a
negative value redraws on every token. The same benchmark after the fixes:
~1.6 ms and ~0.6 MB / ~10k allocations per frame, and frames during
streaming are capped by the throttle.

## Medium impact

5. String accumulation with `+=` for streamed text in `agent/turn.go`
   (`textBuf`, `thinkingBuf`) and `tui/model_agent.go`
   (`Timeline[n-1].Text +=`). Quadratic copying over long responses.
6. Regex-heavy wrapping in `tui/ansi.go`: `WrapText` calls `StripAnsi`,
   `updateActiveSGR` and `ansiEscape.FindStringIndex(s[i:])` per token.
   The unanchored `FindStringIndex` can scan the rest of the string.
7. Lipgloss styles rebuilt on every render in `toolBlockStyle()` and
   `renderToolSummary()`; these can be package-level values.
8. `@` file search spawns `fd`/`find` on every keystroke with no debounce or
   cancellation of the previous search (`tui/model_keys.go`).
9. `ScrollInfo`, `DisableAutoScroll` and `EnsureCursorVisible` walk the full
   timeline on every frame/scroll. Fine once 2 and 3 are fixed; a cached
   total line count would help further.

## Low impact

10. `Tools.Schemas()` rebuilt every turn.
11. Session writer issues two `Write` syscalls per event; a `bufio.Writer`
    would merge them.

## Not a concern

SSE parsing, the guard, and session persistence are already efficient.
