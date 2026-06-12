# Editor Soft Word Wrap Design

## Problem

The `EditorComponent` stores text as logical lines and renders each one directly. Lines longer than the terminal width extend off-screen instead of wrapping.

## Requirements

- **Word wrap** at word boundaries (spaces/punctuation); break mid-word only if a single word exceeds the full width.
- **Soft wrap** -- visual only. The underlying `lines []string` model is unchanged; submitted text has no extra newlines.
- **Visual row navigation** -- Up/Down move through visual rows, not logical lines.

## Approach

Wrap at render time, recompute for navigation. No caching -- chat input text is small enough that recomputing on each keypress is negligible. No new dependencies (runewidth is already a transitive dependency via lipgloss).

## Key data structures

```go
// A visual row -- one screen line produced by wrapping a logical line.
type visualRow struct {
    logicalLine int    // index into e.lines
    startCol    int    // rune offset where this visual row begins
    runeLen     int    // number of runes in this visual row
}
```

## Changes by area

### Wrapping logic (new)

- `wrapLine(line string, width int) []visualRow` -- walks runes, tracks last space position, breaks at word boundaries. Falls back to character break when a word exceeds `width`. Uses `runewidth.RuneWidth()` for wide Unicode (CJK, emoji).
- `buildVisualLayout() []visualRow` -- iterates `e.lines`, calls `wrapLine` for each, concatenates results.
- Wrapping width is `e.width - 1` to leave room for the reverse-video cursor character at line end.

### Render (modified)

- Iterate visual rows from `buildVisualLayout()` instead of `e.lines[scrollTop:end]`.
- Scrolling becomes visual-row based: `scrollTop` counts visual rows, not logical lines.
- `renderWithCursor` takes the visual row's text and the cursor's offset within that row.

### Cursor navigation (modified)

- **Up/Down:** Build visual layout, find the current visual row index, move to the row above/below, map back to logical (line, col). Preserve a "desired column" so vertical movement through rows of different widths feels natural.
- **Left/Right:** Unchanged.
- **Home/End (Ctrl-A/Ctrl-E):** Unchanged -- operates on logical line.
- **Ctrl-K:** Unchanged -- kills to end of logical line.
- **Ctrl-U:** Unchanged -- kills from start of logical line.

### Scrolling (modified)

- `clampScroll` works on visual row indices instead of logical line indices.
- `maxHeight()` limit counts visual rows shown, not logical lines.
- Scroll indicators (^ and v) based on visual rows above/below viewport.

## What stays the same

- `lines []string` data model.
- `InsertRune`, `InsertNewline`, `InsertText`, `Submit`, `SetValue`, `Value`.
- `HandleInput` for most key types.
- History browsing.
- Border rendering.
- Backspace/Delete.

## Edge cases

- **Empty lines:** Wrap to a single visual row with zero runes.
- **Trailing spaces:** Treated as regular characters.
- **Terminal resize:** Re-wrapping happens naturally on next render.
- **Wide Unicode (CJK, emoji):** Use `runewidth.RuneWidth()` for visual width measurement.

## Testing

- Unit tests for `wrapLine`: short lines, exact-width lines, long words, word boundaries, empty strings, wide characters.
- Unit tests for visual-row cursor navigation: Up/Down through wrapped lines, boundary crossings.
- Update existing snapshot golden files.
