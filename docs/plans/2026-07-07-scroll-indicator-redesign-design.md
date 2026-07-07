# Scroll Indicator Redesign

## Problem

The gutter scrollbar (added in `453990d`) causes visual artifacts and rendering bugs. It works by re-rendering all content at `maxWidth - 1` when overflow is detected, then appending a `┃` thumb or space character to each visible line. This approach is fragile with styled/bordered content and causes rendering issues.

## Decision

Remove the gutter scrollbar entirely. Replace it with two lighter-weight indicators:

1. **Status bar scroll position** -- when the user is scrolled up (auto-scroll disabled), show the scroll position in the status bar (e.g. `Ln 42/280 (15%)`).
2. **Gradient fade on boundary lines** -- dim the first visible line when content overflows above, and the last visible line when content overflows below. This gives an at-a-glance visual cue of continuation without stealing any viewport space.

## Changes

### 1. Remove gutter scrollbar

- In `ChatComponent.Render()` (`chat.go`), delete the `needsGutter` / `renderWidth` logic that re-renders at reduced width, and the gutter-appending loop that adds `┃` or space to visible lines.
- `Render()` always uses `maxWidth` directly. No double `countLines()` call.
- Delete gutter-related tests: `TestScrollbarGutterAppearsWhenOverflow`, `TestNoScrollbarWhenContentFits`, `TestScrollbarThumbPosition`.
- Update golden snapshots if any capture the gutter character.

### 2. Scroll position in status bar

- Add `ScrollInfo(maxWidth int) (currentLine, totalLines int)` to `ChatComponent`. Returns the effective scroll offset and total line count.
- In `statusBar()` (`statusbar.go`), when `chat.IsAutoScroll()` is false, append a scroll position segment: `Ln 42/280 (15%)`. When auto-scroll is active, show nothing extra.
- Uses existing `theme.StatusBar` style.

### 3. Gradient fade edge hints

- In `ChatComponent.Render()`, after slicing visible lines, apply dim ANSI to boundary lines:
  - First visible line gets `ansiDim` prefix if `scrollTop > 0` (content above).
  - Last visible line gets `ansiDim` prefix if content extends below the viewport.
- `ansiDim` stacks with existing ANSI styling (bold, color) in most terminals.
- No fade when content fits entirely in the viewport.

## Alternatives considered

- **Bubble Tea viewport component**: Gets mouse wheel for free but requires significant refactor of the custom line-by-line rendering, caching, and cursor system. High risk of breaking existing interactions.
- **Fix gutter bugs in place**: Smallest diff, but permanently sacrifices 1 column and doesn't address the fundamental fragility of text-appended scrollbar characters with styled content.
