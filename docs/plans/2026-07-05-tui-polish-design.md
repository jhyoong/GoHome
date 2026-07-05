# TUI Polish: Turn Separators and Scrollbar Gutter

## Overview

Two rendering-only polish improvements to the chat area, both scoped to `chat.go`.

## Feature 1: Turn separators

Insert a blank line in the rendered output whenever the timeline transitions between role groups.

### Role groups

- Group "user": `KindUser`
- Group "assistant": `KindAssistant`, `KindThinking`, `KindTool`, `KindStats`
- `KindNotice`: neutral — does not trigger separators

A separator (single blank line) fires when transitioning between group "user" and group "assistant" in either direction.

### Where the logic lives

- `ChatComponent.Render()` — when building the `all` slice, check if the current entry's group differs from the previous entry's group. If so, prepend a blank `""` line before the current entry's lines.
- `ChatComponent.countLines()` — same group-transition check, add 1 for each separator so scroll math stays accurate.
- `EnsureCursorVisible()` — the `cursorTop` accumulation loop needs the same separator logic so cursor-to-line mapping stays correct.
- `entryLineCount()` stays unchanged (per-entry, not per-gap).

### Files

`chat.go` only.

## Feature 2: Scrollbar gutter

When chat content exceeds the visible area, reserve 1 column on the right edge for a scrollbar track.

### Visibility

Only when `totalLines > maxHeight`. When everything fits on screen, no gutter — full width available for content.

### Rendering

After `Render()` produces the visible lines slice, a post-processing step appends 1 character to each line:

- Thumb lines: `"┃"` in dim style
- Track lines: `" "` (space)

### Thumb position

- `thumbStart = scrollTop * maxHeight / totalLines`
- `thumbSize = max(1, maxHeight * maxHeight / totalLines)`
- When `autoScroll` is true, thumb sits at the bottom.

### Width adjustment

When the gutter is active, entries render at `maxWidth - 1` so content doesn't collide with the gutter column.

Flow:
1. Call `countLines(maxWidth - 1)` to check if content overflows.
2. If yes: render at `maxWidth - 1`, append gutter.
3. If no: render at full `maxWidth`, no gutter.

Cache invalidation is already handled — `cacheValid(width)` checks the width parameter, so toggling between `W` and `W-1` naturally invalidates.

### Files

`chat.go` only.

## Testing

- Existing snapshot tests cover rendering changes. Golden files regenerated with `-update`.
- `countLines` and `EnsureCursorVisible` accuracy verified by existing `TestViewportScrollback` and cursor navigation tests.
- Manual verification for scrollbar appearance at different terminal sizes.
