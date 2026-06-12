# Editor Word Wrap Implementation Plan

Design: [2026-06-12-editor-word-wrap-design.md](2026-06-12-editor-word-wrap-design.md)

## Steps

### Step 1: Add visualRow type and wrapLine helper

File: `gohome/internal/tui/editor.go`

- Add `visualRow` struct with `logicalLine`, `startCol`, `runeLen` fields.
- Add `wrapLine(line string, logicalIdx int, width int) []visualRow` that word-wraps a single logical line into visual rows using `uniseg.StringWidth` for wide character support.
- Add `buildVisualLayout(width int) []visualRow` that wraps all `e.lines`.
- Add unit tests for `wrapLine`: short lines, exact-width, long words, word boundaries, empty string, wide chars.

### Step 2: Update Render to use visual rows

File: `gohome/internal/tui/editor.go`

- Change `Render` to call `buildVisualLayout(width)` and iterate visual rows.
- Update `e.width` from the render width parameter so navigation can use it.
- Change `scrollTop` semantics from logical-line index to visual-row index.
- Update `clampScroll` to work with visual rows.
- Adapt `renderWithCursor` to work with the visual row's substring and cursor offset within that row.

### Step 3: Update Up/Down navigation for visual rows

File: `gohome/internal/tui/editor.go`

- In `HandleInput` for KeyUp/KeyDown, build visual layout, find current visual row, move up/down, map back to logical (line, col).
- Add `desiredCol` field to EditorComponent for sticky column behavior during vertical navigation.
- Reset `desiredCol` on horizontal movement, typing, etc.
- Keep history browsing behavior: browse history when pressing Up from the first visual row of the first logical line.

### Step 4: Update existing tests and add new tests

File: `gohome/internal/tui/editor_test.go`

- Add tests for wrapped rendering (long line wraps correctly).
- Add tests for visual row navigation (Up/Down through wrapped rows).
- Update any existing tests that assume unwrapped rendering.
- Run `go test ./gohome/internal/tui/ -update` to regenerate snapshots if needed.

### Step 5: Verify

- Build and run the binary.
- Run full test suite.
- Run linter.
