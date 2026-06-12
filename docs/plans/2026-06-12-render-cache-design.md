# Design: Entry-Level Render Cache with Dirty Flags + Throttle Toggle

## Problem

`ChatComponent.Render()` re-renders every timeline entry on every call. During streaming, this is called per token (~20/sec). With a large conversation, each call parses markdown, runs syntax highlighting, and wraps text for thousands of lines -- only to display ~20 visible. This makes the TUI unresponsive for 10+ minutes on moderate conversations (~10k tokens) and will not scale to the 200k token target.

### Root cause

The hot path during streaming:

1. Agent sends token delta event
2. `handleAgentEvent()` appends delta to last entry
3. `rebuildViewport()` is called (line 157 of model_agent.go)
4. `chat.Render(maxWidth)` iterates ALL timeline entries
5. For each assistant entry: calls `RenderMarkdown()` (Goldmark parse + AST walk + Chroma syntax highlighting)
6. For each entry: calls `WrapText()` (ANSI-aware word wrapping with Unicode grapheme handling)
7. Builds a slice of ALL lines, then slices to the ~20 visible
8. Entire result is discarded on the next token

There is no caching anywhere in this pipeline.

## Solution

Two components: an entry-level render cache (the core fix) and an optional render throttle toggle (safety valve for extreme cases).

### Component 1: Render Cache

Each timeline entry stores its own cached rendered lines alongside the raw content. Rendering only happens when the entry's cache is invalid.

#### Data structure change

Add cache fields to `TimelineEntry`:

```go
type TimelineEntry struct {
    // ... existing fields ...

    // Render cache (unexported -- internal to the TUI)
    cachedLines    []string
    cachedWidth    int
    cachedExpanded bool
    cachedText     string
    cachedResult   string
    dirty          bool
}
```

#### Cache invalidation

An entry is re-rendered only when any of these conditions is true:

- `dirty == true` (explicitly marked by event handlers)
- `cachedWidth != currentWidth` (terminal was resized)
- `cachedExpanded != Expanded` (user toggled expand/collapse)
- `cachedText != Text` (content changed -- catches any mutation path)
- `cachedResult != ToolResult` (tool result arrived or changed)

#### Where dirty is set

- `handleAgentEvent` for `EventTokenDelta` / `EventThinkingDelta`: the last entry in the timeline gets `dirty = true`
- `EventToolResult`: the matched tool entry gets `dirty = true`
- `EventThinkingDone`: the collapsed thinking entry gets `dirty = true`
- Toggle expand/collapse: automatically caught by the `cachedExpanded` check (no explicit dirty flag needed)

#### Render() change

```
for each entry:
    if entry has valid cache (same width, expanded, text, result):
        use cachedLines
    else:
        render entry (markdown/wrap/highlight)
        store result + inputs in cache fields
        clear dirty flag
    append to all[]
slice all[] to visible window
```

#### countLines() change

Same caching logic -- use `len(cachedLines)` when cache is valid instead of re-rendering. This is called from `DisableAutoScroll()` which currently does a full markdown parse of every entry just to count lines.

### Component 2: Render Throttle Toggle

#### Config addition

```go
type Settings struct {
    // ... existing ...
    RenderThrottleMs int `json:"renderThrottleMs,omitempty"`
}
```

- `0` = per-token rendering (current behavior, now fast with caching). This is the default.
- Any positive value (e.g. `50`) = minimum milliseconds between render triggers during streaming.

#### Implementation

In `handleAgentEvent`, when processing `EventTokenDelta` or `EventThinkingDelta`:

- If throttle is 0: call `rebuildViewport()` immediately (current behavior)
- If throttle > 0: skip `rebuildViewport()` if less than N ms since last call. Schedule a deferred render via `tea.Tick` to ensure the final state is always rendered after the last delta arrives.

New fields on `Model`:

```go
renderThrottleMs  int
lastRenderTime    time.Time
renderPending     bool
```

## What stays the same

- The "render all entries, slice to visible window" model stays. With caching, building the full `all[]` slice is cheap (concatenating cached string slices).
- The scroll system, cursor, expand/collapse logic, and all TUI interactions are unchanged.
- The `View()` function in `model.go` is unchanged -- it still calls `m.chat.Render()`.
- Markdown rendering, syntax highlighting, and text wrapping code is unchanged -- it is just called less often.

## Scope explicitly excluded

- Virtual viewport (only render visible entries) -- not needed with caching. Could be a future optimization if conversations exceed 200k tokens.
- Background render worker -- not needed. Would fight Bubble Tea's single-threaded model.
- Markdown parse tree caching -- entry-level caching makes this unnecessary since each entry's rendered output is cached as a whole.

## Key files to modify

- `gohome/internal/tui/model.go` -- `TimelineEntry` struct (add cache fields), plumb throttle config, add throttle state fields to `Model`
- `gohome/internal/tui/chat.go` -- `Render()`, `countLines()` (use cache)
- `gohome/internal/tui/model_agent.go` -- set dirty flags in `handleAgentEvent()`, throttle logic
- `gohome/internal/config/config.go` -- add `RenderThrottleMs` to `Settings`
- `gohome/internal/config/defaults.go` -- default value (0)
- `gohome/internal/config/config.go` -- merge logic for new field

## Performance expectation

With caching, a streaming token delta in a 200k-token conversation should only re-render the single active entry (~100ms worst case for a very long single response with many code blocks) instead of re-rendering all entries (~minutes). The `all[]` slice assembly becomes O(n) string slice copies with no parsing, which is negligible.
