# TUI Architecture Analysis

This document captures a full analysis of the `packages/tui` framework and its consumer
`packages/coding-agent/src/modes/interactive`, intended as a reference for building a Go equivalent.

---

## 1. Core Rendering Engine

**Architecture:** Retained-mode component tree with line-level differential rendering.

**Component interface** -- the single contract everything implements:

```
render(width int) []string   // return terminal lines
handleInput(data string)     // receive key events (optional)
invalidate()                 // clear render caches (optional)
```

**Render pipeline** (`tui.ts` -> `doRender()`):

1. **Tree traversal** -- `Container.render()` concatenates all children's line arrays top-to-bottom.
2. **Overlay compositing** -- overlays are spliced character-by-character onto the base lines via `compositeLineAt()`, preserving ANSI codes.
3. **Cursor extraction** -- scans for a special APC marker (`\x1b_pi:c\x07`) placed by the focused component to position the hardware cursor for IME.
4. **Line normalization** -- appends SGR reset + OSC8 hyperlink close to every line.
5. **Diffing** -- string equality (`===`) between `newLines[]` and `previousLines[]`. Only changed lines are rewritten. Falls back to full clear+redraw on terminal resize or content shrinkage.

All output is buffered into a single `write()` call wrapped in synchronized output mode (`\x1b[?2026h` ... `\x1b[?2026l`) to prevent flicker.

**Render loop** is demand-driven, not a fixed ticker:

- `requestRender()` -> coalesced via `process.nextTick` -> rate-limited to 16ms minimum between renders (~60fps cap).
- No fixed game loop -- renders only happen when state changes.

**Terminal abstraction** (`terminal.ts`):

- Raw mode via `stdin.setRawMode(true)`.
- Bracketed paste mode (`\x1b[?2004h`).
- Kitty keyboard protocol negotiation with 150ms fallback to `modifyOtherKeys`.
- Standard ANSI sequences for cursor movement, line clearing, screen clearing.

**No scroll layer** -- the terminal's own scrollback is the only scrollback. The TUI appends lines downward and lets the terminal scroll naturally. Individual components (like `SelectList`) implement their own internal scroll viewports.

### Key Data Structures

- `previousLines: []string` -- the virtual framebuffer, used for diffing on every render.
- `previousWidth`, `previousHeight` -- terminal dimensions from last render.
- `cursorRow`, `hardwareCursorRow` -- logical vs physical cursor position.
- `maxLinesRendered` -- high-water mark of lines rendered (used to detect content shrinkage).
- `previousViewportTop` -- how many lines have scrolled above the visible terminal window.
- `focusedComponent` -- which component receives key input.
- `overlayStack[]` -- ordered list of active overlays with layout options and focus state.

### AnsiCodeTracker

Stateful parser that tracks active SGR (color/style) codes and OSC8 hyperlinks as it processes a string. Used by `wrapTextWithAnsi()` to re-emit active ANSI codes at the start of wrapped lines, ensuring styles do not break across line boundaries.

---

## 2. Input Handling and Keybindings

**Input pipeline:**

```
raw stdin -> StdinBuffer (sequence assembler, 10ms timeout) -> Terminal -> TUI.handleInput()
    -> inputListeners middleware chain -> focusedComponent.handleInput(data)
```

**Key event model** -- there is no typed key event struct. The raw escape sequence string is passed everywhere. Matching is done by `matchesKey(rawString, "modifier+keyname")` which tries multiple representations in priority order:

1. Kitty CSI-u sequences
2. xterm modifyOtherKeys
3. Legacy escape sequences from lookup tables
4. Raw control characters (e.g., `ctrl+c` -> `\x03`)
5. Legacy ESC-prefix (e.g., `alt+a` -> `\x1ba`)

### StdinBuffer

An EventEmitter that accumulates raw stdin bytes and emits complete escape sequences. Uses a 10ms timeout to flush partial sequences. Handles CSI, OSC, DCS, APC, SS3 sequences, old-style mouse, SGR mouse, and bracketed paste (emits a separate `paste` event with content stripped of markers). Also deduplicates Kitty printable codepoint events.

High-byte compatibility: single bytes > 127 are rewritten as `\x1b` + `(byte - 128)` to normalize legacy terminal meta encoding.

### Keybinding System

- Bindings are dot-namespaced strings: `"tui.editor.cursorLeft"`, `"app.model.select"`.
- Each binding maps to one or more `KeyId` values: `["left", "ctrl+b"]`.
- `KeyId` format: `modifier+modifier+keyname` (e.g., `"ctrl+c"`, `"shift+alt+enter"`).
- Modifier bitmask values: `shift=1, alt=2, ctrl=4, super=8`. Lock bits (64+128) are stripped.
- User overrides completely replace the default key list per binding ID.
- Setting a binding to `undefined` disables it.
- Conflict detection at rebuild time (non-blocking).
- Components do their own matching: `getKeybindings().matches(data, "binding.name")` -- no event dispatch table or callback registry.
- Extension point via TypeScript declaration merging (downstream packages add new binding IDs).

### Dispatch Layers

**Layer 1 -- TUI global handlers** (`TUI.handleInput()`):
- `inputListeners` middleware chain -- can consume or transform events.
- Hard-coded global: `shift+ctrl+d` triggers debug.
- Terminal system sequences consumed here.
- Key release events filtered unless component opts in (`wantsKeyRelease`).

**Layer 2 -- Focused component** (`Component.handleInput(data)`):
- Only one component receives input at a time.
- Overlays capture focus when shown (unless `nonCapturing`).
- No bubbling, no capture phase.

### Emacs-Style Editing

**Kill ring** (`kill-ring.ts`):
- Plain `string[]` stack, no max size.
- `push(text, { prepend, accumulate })` -- accumulate merges with last entry for consecutive kills, prepend controls direction.
- `peek()` returns most recent, `rotate()` moves last to front (for yank-pop).
- Components track `lastAction` to determine accumulation.

**Word navigation** (`word-navigation.ts`):
- Pure functions: `findWordBackward(text, cursor)` and `findWordForward(text, cursor)`.
- Uses `Intl.Segmenter` with `granularity: "word"` for Unicode correctness.
- Handles punctuation sub-boundaries within word segments.
- Configurable via `WordNavigationOptions` (custom segmenter, atomic segment predicate).

**Undo stack** (`undo-stack.ts`):
- Clone-on-push stack (uses `structuredClone`), undo-only (no redo).
- Coalescing: consecutive word-character insertions share one undo point; whitespace and edit operations each create their own.

### Non-Latin Keyboard Support

Kitty protocol's `baseLayoutKey` field is used as a fallback for keybinding matching, so `Ctrl+C` works regardless of keyboard layout. The parser normalizes shifted letters (maps uppercase back to lowercase when Shift is in the modifier bitmask).

---

## 3. Component Library

### Layout Primitives

**Box** (`components/box.ts`):
- Padding container with `paddingX` (default 1) and `paddingY` (default 1).
- Optional `bgFn: (text) => string` background color function applied to every line including padding.
- Background extends to full terminal width regardless of content length.
- Render cache keyed on `(childLines, width, bgSample)`.
- `invalidate()` propagates recursively to children.

**Spacer** (`components/spacer.ts`):
- Emits N blank lines (empty strings, not space-padded).

**Container** (internal, part of `tui.ts`):
- Ordered list of `Component` children via `addChild()`, `removeChild()`, `clear()`.
- `render(width)` concatenates all children's line arrays.

### Text Display

**Text** (`components/text.ts`):
- Word-wrapped text with ANSI code preservation via `wrapTextWithAnsi`.
- Props: `text`, `paddingX` (default 1), `paddingY` (default 1), optional `customBgFn`.
- Tabs replaced with 3 spaces. Returns `[]` for empty/whitespace-only text.
- Render cache keyed on `(text, width)`.

**TruncatedText** (`components/truncated-text.ts`):
- Single-line text, truncated to fit width (never wraps).
- Props: `text`, `paddingX` (default 0), `paddingY` (default 0).
- Uses ANSI-aware `truncateToWidth()`.

**Markdown** (`components/markdown.ts`):
- Full markdown renderer using the `marked` library with custom tokenizer.
- 17 theme properties covering headings, links, code, quotes, lists, syntax highlighting, etc.
- `DefaultTextStyle` with color, bgColor, bold, italic, strikethrough, underline.
- Supported blocks: heading (h1=bold+underline, h2=bold, h3+=plain), paragraph, code block, list (ordered/unordered/task), table, blockquote, hr, html, space.
- Tables: proportional column width distribution, box-drawing borders, multi-line cell wrapping, raw markdown fallback if terminal too narrow.
- Links: OSC 8 clickable hyperlinks when supported, otherwise `(URL)` suffix.
- Style restoration: uses a sentinel character trick to restore parent ANSI styles after inline resets.
- Render cache keyed on `(text, width)`.

### Input Components

**Input** (`components/input.ts`):
- Single-line text field with horizontal scrolling.
- State: `value: string`, `cursor: number` (byte offset).
- Renders as `"> [text]"` with reverse-video cursor character.
- Full Emacs editing: kill ring, word navigation, undo, yank/yank-pop.
- Grapheme-aware cursor movement via `Intl.Segmenter`.
- Bracketed paste: strips newlines/CR, normalizes tabs to 4 spaces.
- Kitty keyboard protocol: decodes CSI-u printable sequences.
- Implements `Focusable`: emits `CURSOR_MARKER` when focused.

**Editor** (`components/editor.ts`):
- Multi-line editor with word wrapping and vertical scrolling.
- State: `lines: string[]`, `cursorLine: number`, `cursorCol: number`.
- Bordered with `---` lines showing scroll indicators (`--- up N more ---`, `--- down N more ---`).
- Max visible lines = `max(5, floor(terminalRows * 0.3))`.
- Sticky column vertical movement (7-case decision table for preferred column tracking).
- Paste markers: large pastes (>10 lines or >1000 chars) collapsed to `[paste #N +M lines]`, stored in a `Map<number, string>`, expanded on submission.
- History: up to 100 entries, up/down arrow navigation when on first/last visual line.
- Character jump mode: press binding then next printable char jumps to first occurrence.
- Autocomplete integration: debounced 20ms for symbol autocomplete, immediate for slash commands. Tab accepts, request cancellation via AbortController.
- Word-wrap algorithm: iterates graphemes, tracks wrap opportunities at whitespace, force-breaks on overflow.

### List Components

**SelectList** (`components/select-list.ts`):
- Scrollable single-selection list.
- Items: `{value, label, description?}`.
- Two-column layout: primary column auto-calculated from widest item (clamped to min/max), description shown when `width > 40`.
- Selected item: `"-> " + selectedText(label)`, unselected: `"   " + label`.
- Scroll indicator `(current/total)` when items exceed `maxVisible`.
- Filtering: case-insensitive prefix matching via `setFilter()`.
- Navigation: up/down with wrap-around, Enter to confirm, Escape to cancel.

**SettingsList** (`components/settings-list.ts`):
- Key/value settings panel with label/value pairs.
- Optional search via fuzzy filtering.
- Value cycling: Enter/Space cycles through `values[]` array.
- Submenu delegation: `submenu` function returns a Component that fully takes over render and input.
- Aligned layout: labels padded to `min(30, widest label)`.
- Description area below list for selected item.

### Feedback Components

**Loader** (`components/loader.ts`):
- Animated braille spinner `["braille","braille","braille",...]` at 80ms interval.
- Extends `Text`, prepends a blank line for visual separation.
- `start()`/`stop()` control the animation timer.
- `setMessage()` updates text and triggers immediate re-render.

**CancellableLoader** (`components/cancellable-loader.ts`):
- Extends `Loader` with Escape-to-abort support.
- Exposes `signal: AbortSignal` for passing to async operations.
- `handleInput` listens for cancel keybinding.

**Image** (`components/image.ts`):
- Kitty graphics protocol or iTerm2 inline images, with text fallback.
- Props: `base64Data`, `mimeType`, `maxWidthCells` (default 60), `maxHeightCells`, `filename`, `imageId`.
- Kitty: single image transmission line + empty lines for height. Uses `C=1` flag.
- iTerm2: empty lines + cursor-up trick for correct cursor accounting.
- Fallback: `"[type: WxH filename]"` in themed color.

### EditorComponent Interface

Abstract interface for pluggable editor implementations:
- Required: `getText()`, `setText()`, `handleInput()`, `onSubmit`, `onChange`, `render()`.
- Optional: `addToHistory()`, `insertTextAtCursor()`, `getExpandedText()` (expands paste markers), `setAutocompleteProvider()`, `borderColor`, `setPaddingX()`, `setAutocompleteMaxVisible()`.

### Autocomplete System

**CombinedAutocompleteProvider** combining multiple sources:

1. **Slash commands** -- triggered by `/` at start, fuzzy filtered on command names.
2. **File path completion (Tab)** -- synchronous `readdirSync`, directories first, alphabetical sort.
3. **`@`-prefix fuzzy file search** -- uses `fd` subprocess respecting `.gitignore`, scored by match quality (exact name 100, starts-with 80, substring in name 50, substring in path 30), 20ms debounce.
4. **Slash command argument completion** -- delegated to per-command `getArgumentCompletions()`.
5. **Extension-registered commands**.

Quoted path support: `@"path with spaces"` and `@path` variants.

### Fuzzy Matching (`fuzzy.ts`)

Sequential character matching (all query chars must appear in order in text). Case-insensitive.

Scoring (lower = better):
- Consecutive matches: `-5 * consecutiveCount` bonus.
- Gap penalty: `+(gap_length * 2)`.
- Word boundary bonus: `-10` when match is after `[\s\-_./:]`.
- Position penalty: `+(i * 0.1)`.
- Exact match bonus: `-100`.
- Swapped alphanumeric fallback: tries `[letters][digits]` as `[digits][letters]` (+5 penalty).

Multi-token queries (space-separated): ALL tokens must match (AND logic). Results sorted ascending by total score.

---

## 4. Interactive Chat Mode

### Layout Structure

Vertical stack of named containers added via `ui.addChild()`:

```
headerContainer          -- app logo + keybinding hints
chatContainer            -- all historical messages (append-only, unbounded)
pendingMessagesContainer -- queued follow-up messages preview
statusContainer          -- loading spinner / compaction / retry
widgetContainerAbove     -- extension widgets above editor
editorContainer          -- swappable: editor OR selector/dialog
widgetContainerBelow     -- extension widgets below editor
footer                   -- always-visible 2-3 line status bar
```

### Message Rendering

**User messages** (`UserMessageComponent`):
- Markdown in a Box with `userMessageBg` background.
- OSC 133 shell integration markers injected at first and last lines.

**Assistant messages** (`AssistantMessageComponent`):
- Content container rebuilt entirely on each `updateContent()` call (streaming).
- Text blocks: Markdown with paddingX=1.
- Thinking blocks: collapsible -- either italic `"Thinking..."` placeholder or full Markdown in italic.
- Stop reason: error/abort messages appended in red.

**Tool executions** (`ToolExecutionComponent`):
- Each tool call is its own component appended to `chatContainer`.
- Three background color states: pending (`toolPendingBg`), success (`toolSuccessBg`), error (`toolErrorBg`).
- Two rendering shells: `"default"` (content in a Box) or `"self"` (tool controls its own frame).
- Custom `renderCall` and `renderResult` functions per tool type.
- Collapsible via `setExpanded()`.
- Image results rendered as `Image` components below the tool box.
- `pendingTools: Map<string, ToolExecutionComponent>` tracks in-progress tools.

### Streaming

Event-driven via agent session event bus:

1. `agent_start` -- clear pending tools, show loading spinner.
2. `message_start` (role=assistant) -- create `AssistantMessageComponent`, store as `streamingComponent`.
3. `message_update` -- call `streamingComponent.updateContent(message)` (full inner rebuild, TUI diff handles efficiency).
4. `tool_execution_start` -- mark tool component as execution-started.
5. `tool_execution_update` -- partial results via `updateResult(..., isPartial=true)`.
6. `tool_execution_end` -- final result via `updateResult(..., isPartial=false)`.
7. `message_end` -- finalize streaming component, call `setArgsComplete()` on pending tools.
8. `agent_end` -- stop spinner, clear status.

### Footer (`FooterComponent`)

Renders 2-3 lines:

- **Line 1 (pwd):** `~/project/path (branch) . session-name`.
- **Line 2 (stats):** Token counts (`up123k down45k R12k W8k $0.04`) + context % (colored at 70%/90% thresholds) + `(auto)` if auto-compaction + model name + thinking level. Right-aligned.
- **Line 3 (optional):** Extension status entries, sorted by key.

### Theming System

- 54 foreground colors + 6 background colors, named semantically.
- Color values: hex (`"#ff0000"`), 256-color integer, variable reference, or empty string (terminal default).
- Truecolor auto-downsampled to nearest 256-color cube entry via weighted Euclidean distance.
- Global singleton via `Proxy` on `globalThis[Symbol.for(...)]`.
- Hot-reload file watcher on custom themes with 100ms debounce.
- Applied via `theme.fg("colorName", text)` -> ANSI wrapping (`\x1b[38;2;R;G;Bm` or `\x1b[38;5;Nm`).

### Editor Area (Swappable Slot)

`editorContainer` holds exactly one child at a time -- either the input editor or a selector/dialog.

On open selector: `editorContainer.clear()` -> `addChild(selector)` -> `setFocus(selector)`.
On close: `editorContainer.clear()` -> `addChild(editor)` -> `setFocus(editor)`.

### Input Features

- `!command` prefix -> bash mode (border color changes to `bashMode` theme color).
- `Shift+Enter` -> newline, `Enter` -> submit.
- `Alt+Enter` while streaming -> queue follow-up message.
- `Enter` while streaming -> steer current response.
- External editor: writes to temp file, spawns `$VISUAL`/`$EDITOR`, reads back on exit.
- Clipboard image paste (`Ctrl+V`).
- History: up/down arrow cycles through submitted messages.

### State Machine

Main loop:
```
while (true) {
    text = await getUserInput()   // Promise resolved on Enter
    await session.prompt(text)    // blocks until agent cycle completes
}
```

Key state variables:
- `isStreaming` -- whether the LLM is actively generating.
- `isBashMode` -- whether editor text starts with `!`.
- `toolOutputExpanded` -- whether expandable items are expanded.
- `hideThinkingBlock` -- thinking blocks as placeholder or full content.
- `streamingComponent` -- currently-streaming AssistantMessageComponent.
- `pendingTools` -- map of in-progress tool components.

Session rebinding: on session change (new/fork/resume), all containers are cleared and rebuilt from session history.

---

## 5. Overlay and Selector System

### Overlay Stack

Flat array on the TUI, composited at render time:

```
overlayStack: {
    component: Component
    options?: OverlayOptions
    preFocus: Component | null   // focus target before this overlay
    hidden: boolean
    focusOrder: number           // monotonically increasing, higher = on top
}[]
```

### Capturing vs Non-Capturing

- **Capturing (modal, default):** steals keyboard focus immediately. Component handles Escape to call `handle.hide()`.
- **Non-capturing:** renders on screen without stealing focus. Focus can be manually transferred with `handle.focus()` and given back with `handle.unfocus()`.

### Z-Ordering

`focusOrder` determines visual layering. Higher = rendered last = on top. `handle.focus()` bumps the entry's `focusOrder` to be highest. `compositeOverlays` sorts visible entries by `focusOrder` ascending before compositing.

### Focus Management

Single `focusedComponent` pointer.

- On show: snapshot current focus as `preFocus`, set focus to overlay component.
- On hide: restore `preFocus`, or pass to next topmost visible capturing overlay.
- On `setHidden(true/false)`: same transfer logic.
- On focused overlay becoming invisible (via `visible()` callback): TUI redirects focus to topmost visible capturing overlay.
- Nested overlays: fully supported. Any overlay's `handleInput` can call `showOverlay()` to push another.

### Positioning (OverlayOptions)

```
width:     number | "50%"        // absolute columns or percentage
minWidth:  number                // minimum after percentage
maxHeight: number | "50%"        // truncate overlay lines
anchor:    "center" | "top-left" | "top-right" | "bottom-left" | "bottom-right"
           | "top-center" | "bottom-center" | "left-center" | "right-center"
offsetX:   number                // pixel offset after anchor
offsetY:   number
row:       number | "25%"        // override anchor with absolute position
col:       number | "25%"
margin:    number | {top, right, bottom, left}   // shrinks placement area
visible:   (w, h) => boolean     // dynamic visibility callback
nonCapturing: boolean
```

Two-pass layout: first pass resolves width (renders component), second pass resolves height for final row/col.

### Compositing

`compositeLineAt()` does single-pass column-level string splicing:
1. Extract "before" segment of base line (columns 0 to overlay start).
2. Insert overlay line content.
3. Extract "after" segment of base line (columns overlay end to line end).
4. Concatenate `before + overlay + after`.

`extractSegments` and `sliceByColumn` helpers handle ANSI escape sequences as zero-width characters.

### Selector Pattern

Every selector follows this structure:

1. **Outer component** (`extends Container implements Focusable`):
   - Builds component tree: `DynamicBorder` / `Spacer` / header / search input / list / `Spacer` / `DynamicBorder`.
   - Implements `Focusable` by delegating `focused` to whichever child owns the cursor.
   - Callbacks: `onSelect(item)`, `onCancel()`.

2. **Inner list class** (private, `implements Component, Focusable`):
   - Holds `allItems`, `filteredItems`, `selectedIndex`, `searchInput`, `maxVisible`.
   - `render(width)`: search input + visible slice + scroll indicator.
   - `handleInput(data)`: up/down/pageUp/pageDown/confirm/cancel, else forward to search input and re-filter.
   - Viewport centering: `startIndex = max(0, min(selectedIndex - floor(maxVisible/2), total - maxVisible))`.

3. **Show from caller:**
   - Editor-area replacement: clear `editorContainer`, add selector, set focus. On done/cancel, restore editor.
   - True overlay: `tui.showOverlay(component, options)`. On cancel, `handle.hide()`.

### Selector Inventory

**Session selector** -- most complex:
- Two scopes (current folder / all) switchable with Tab.
- Three sort modes: threaded (tree hierarchy), recent (time-ordered), relevance (fuzzy score).
- Named filter: show only named sessions.
- Hierarchical tree display with ASCII box-drawing connectors.
- Multi-level search: fuzzy tokens, quoted exact phrases, regex (`re:<pattern>`).
- Delete with red confirm state, inline rename mode.

**Model selector:**
- Two scopes (scoped / all) via Tab.
- Async model loading, wrapping navigation.
- Current model displayed first.

**Config selector:**
- 3-level hierarchy: Groups -> Subgroups -> Items.
- Space bar toggles checkboxes. Non-selectable group/subgroup headers.
- Search filters items but preserves group headers for context.

**Settings selector:**
- Uses `SettingsList` primitive.
- Two activation modes: value cycling or submenu delegation.
- Live preview for theme selection via `onSelectionChange`.
- Conditional items based on terminal capabilities.

**Tree selector:**
- Full ASCII-art conversation tree with folding (`fold`/`unfold` indicators).
- Active path highlighted with dot markers.
- 5 filter modes: default, no-tools, user-only, labeled-only, all.
- Inline label editing with dedicated `LabelInput` component.
- Free-text search: printable chars append to query, backspace removes.

**Other selectors:**
- **Theme selector** -- thin wrapper around `SelectList` with live preview via `onSelectionChange`.
- **Thinking selector** -- `SelectList` with 6 items and token estimate descriptions.
- **Extension selector** -- optional countdown timer, vim-style j/k navigation.
- **Extension editor** -- wraps `Editor` with external editor launch support.
- **Extension input** -- single-line `Input` with optional countdown timer.
- **OAuth selector** -- mode-aware (login/logout), auth status display per provider.
- **Login dialog** -- stateful content area updated by OAuth flow, promise-based input, auto-opens URLs.
- **User message selector** -- two-line items (message + metadata), starts at most recent.
- **Scoped models selector** -- reorder via keybindings, toggle provider, enable-all/clear-all, unsaved state tracking.
- **Show images selector** -- two-item boolean toggle.

---

## 6. Go Implementation Reference

| Concept | Go Approach |
|---|---|
| Component interface | `Render(width int) []string`, `HandleInput(data string)`, `Invalidate()` |
| Terminal raw mode | `golang.org/x/term` package |
| ANSI width calculation | `github.com/rivo/uniseg` for grapheme clustering + East Asian width |
| Render loop | Rate-limited channel or `time.AfterFunc` with 16ms minimum |
| Stdin parsing | Goroutine-based sequence assembler with 10ms timeout channel |
| Diff rendering | `[]string` equality comparison, rewrite only changed lines |
| Synchronized output | Wrap each render in `\x1b[?2026h` ... `\x1b[?2026l` |
| Overlay compositing | Column-level string splicing with ANSI code preservation |
| Focus management | Single `atomic.Pointer[Component]` or mutex-protected field |
| Theming | Package-level pointer swappable at runtime, `theme.Fg("name", text)` |
| Keybindings | Map of `string -> []KeyId`, user overrides replace defaults entirely |
| Kill ring | `[]string` stack with accumulate/rotate operations |
| Markdown rendering | `github.com/yuin/goldmark` as the parser, custom ANSI renderer |
| Word segmentation | `github.com/rivo/uniseg` (Go lacks `Intl.Segmenter`) |
| Fuzzy matching | Port the scoring algorithm directly (sequential char match) |
| Bracketed paste | Detect `\x1b[200~` ... `\x1b[201~` in stdin buffer |
| Kitty protocol | Negotiation query on start, 150ms fallback timer |
