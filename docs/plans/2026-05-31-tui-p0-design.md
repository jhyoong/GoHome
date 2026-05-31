# TUI P0 Gaps -- Design

**Status:** Approved 2026-05-31.
**Scope:** All five P0 gaps from the TUI gap analysis -- markdown rendering, multi-line editor, slash command implementations, animated spinner, and input history.
**Approach:** Thin component layer on Bubbletea. Decompose the monolithic Model into distinct components while keeping Bubbletea for the event loop and terminal I/O.

---

## 1. Component Interface and Root Model Refactor

### Interface

```go
// Component is the rendering contract for all TUI elements.
type Component interface {
    Render(width int) []string
}

// Interactive is a Component that can receive input.
type Interactive interface {
    Component
    HandleInput(msg tea.KeyMsg) tea.Cmd
}
```

No `invalidate()` needed -- Bubbletea re-renders on every Update.

### Root Model as Dispatcher

`Model.View()` concatenates component renders:

```go
func (m *Model) View() string {
    lines := []string{}
    lines = append(lines, m.strip.Render(m.winW)...)
    lines = append(lines, m.chat.Render(m.winW)...)
    lines = append(lines, m.spinner.Render(m.winW)...)
    lines = append(lines, m.editor.Render(m.winW)...)
    lines = append(lines, m.statusBar.Render(m.winW)...)
    return strings.Join(lines, "\n")
}
```

### Input Routing

1. Handle global keys (Ctrl+C, Ctrl+]/[).
2. If an overlay is active, route to overlay.
3. Otherwise route to `m.focused.HandleInput(msg)`.

### Component Inventory

| Component | Type | Responsibility |
|---|---|---|
| `StripComponent` | `Component` | Session tabs at top |
| `ChatComponent` | `Component` | Scrollable message history with markdown |
| `SpinnerComponent` | `Component` | Animated braille dots when in-flight |
| `EditorComponent` | `Interactive` | Multi-line input with history |
| `StatusBarComponent` | `Component` | Bottom bar with tokens, model, progress |
| `ApprovalOverlay` | `Interactive` | Approval prompt (existing logic, new interface) |

### File Layout

```
internal/tui/
  component.go       # Interface definitions
  model.go           # Root Model (dispatcher, event routing)
  chat.go            # ChatComponent
  editor.go          # EditorComponent
  spinner.go         # SpinnerComponent
  strip.go           # StripComponent (existing, adapted)
  statusbar.go       # StatusBarComponent (existing, adapted)
  approval.go        # ApprovalOverlay (existing, adapted)
  markdown.go        # Goldmark ANSI renderer
  ansi.go            # ANSI text utilities (wrap, truncate, width)
  history.go         # Input history ring buffer
  slash.go           # Slash command handlers
```

---

## 2. Markdown Rendering

### Approach

Goldmark parser with a custom ANSI terminal renderer. Not glamour -- it adds too many dependencies and doesn't give line-level control.

### Dependencies

- `github.com/yuin/goldmark` -- parser
- `github.com/alecthomas/chroma/v2` -- syntax highlighting for code blocks

### Block Rendering

| Block | Rendering |
|---|---|
| Heading h1 | Bold + underline |
| Heading h2 | Bold |
| Heading h3+ | Plain, prefixed with `###` |
| Paragraph | Word-wrapped with ANSI preservation |
| Code block | Indented 2 spaces, syntax highlighted via chroma, fenced with `---` |
| Inline code | Reverse video or dim background |
| Bold / Italic | ANSI bold / italic sequences |
| Lists | `- ` / `1. ` prefix, indented nesting |
| Links | Text shown normally, URL appended in dim if not obvious |
| Tables | Column-aligned with box-drawing characters, proportional widths |
| Blockquote | `| ` prefix in dim |
| Horizontal rule | Full-width `---` |

### API

```go
func RenderMarkdown(source string, width int) []string
```

Parses with goldmark, walks the AST, emits `[]string` (terminal lines). Tracks active SGR state so styles don't bleed across lines.

### ANSI Text Utilities

```go
// ansi.go
func WrapText(text string, width int) []string       // word-wrap preserving ANSI
func TruncateText(text string, width int) string     // ANSI-aware truncation
func VisualWidth(text string) int                     // display width ignoring escapes
```

Uses `github.com/rivo/uniseg` for grapheme clustering and East Asian width. Replaces the buggy `s[:57]` byte slicing in `shortSummary()`.

---

## 3. Multi-line Editor

### Replaces

The current `textarea.Model` (fixed 3 rows from Bubbletea's bubbles library).

### Key Behaviors

- **Dynamic height:** min 3 lines, max `floor(terminalHeight * 0.3)`. Grows as content grows.
- **Word wrapping:** Soft wrap at word boundaries. Cursor movement is visual-line aware.
- **Vertical scroll:** Scroll indicators when content exceeds max height.
- **Submit:** Enter submits. Shift+Enter / Alt+Enter inserts a newline.
- **Border:** Top/bottom `---` lines. Border color changes in bash mode (`!` prefix).

### State

```go
type EditorComponent struct {
    lines      []string   // logical lines (before wrapping)
    cursorLine int
    cursorCol  int
    scrollTop  int        // first visible wrapped line
    history    *History   // ring buffer of past inputs
    width      int        // last known render width
    maxHeight  int        // computed from terminal height
}
```

### Keybindings

| Key | Action |
|---|---|
| Enter | Submit |
| Shift+Enter / Alt+Enter | Insert newline |
| Up (first visual line) | History previous |
| Down (last visual line) | History next |
| Up/Down (mid-content) | Move cursor vertically |
| Left/Right | Move cursor (wrap across lines) |
| Ctrl+A / Home | Start of line |
| Ctrl+E / End | End of line |
| Ctrl+K | Kill to end of line |
| Ctrl+U | Kill entire line |
| Backspace/Delete | Standard |

### Input History

```go
type History struct {
    entries []string  // up to 100
    pos     int       // current position (-1 = not browsing)
    draft   string    // saved current input when browsing starts
}
```

Up on first visual line: save current text as `draft`, set `pos = len(entries)-1`. Down past end restores `draft`.

---

## 4. Spinner

### Behavior

Braille animation (`⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`) cycling at 80ms. Renders a single line between chat and editor. Disappears when session is not in-flight (zero-height render).

### State

```go
type SpinnerComponent struct {
    frame   int
    active  bool
    message string
    ticker  *time.Ticker
}
```

Sends `spinnerTickMsg` on each tick. Root Model forwards to spinner which increments `frame`.

### Message Updates

- Default: `"Thinking..."`
- After first tool call: `"Running <toolName>..."`
- Multiple tools: `"Running tools..."`

Starts on `EventTokenDelta`, stops on `EventTurnDone` or `EventError`.

---

## 5. Slash Commands

### Callback Pattern

```go
type SlashCallbacks struct {
    NewSession    func() (string, error)
    ResumeSession func(id string) error
    CancelSession func(id string)
    SetModel      func(name string) error
}
```

Injected into Model at construction. Keeps TUI decoupled from agent/session packages.

### Command Implementations

**`/cancel`** -- Calls `CancelSession(focusedID)`. Sets `InFlight = false`. Appends notice: `"Cancelled."`.

**`/new`** -- Calls `NewSession()`. Adds returned session ID to `order`, focuses it, clears editor.

**`/resume <id>`** -- Calls `ResumeSession(id)`. On success, adds to `order` and focuses. On error, shows in `statusMsg`.

**`/model <name>`** -- Calls `SetModel(name)`. Updates `modelName` on success. Without argument, shows current model in `statusMsg`.

---

## 6. ChatComponent

### Structure

```go
type ChatComponent struct {
    timeline  *[]TimelineEntry
    scrollTop int
    maxHeight int
}
```

### Rendering Changes

- Assistant entries pass through `RenderMarkdown()`.
- User entries get a styled "you:" prefix.
- Tool entries gain a `Status` field (`"pending"` | `"done"` | `"error"`) with corresponding background tint.
- Independent scroll. PgUp/PgDown adjust `scrollTop`. Auto-scroll-to-bottom on new content unless user has scrolled up.

### Viewport Replacement

The Bubbletea `viewport.Model` is replaced by `ChatComponent` doing its own scroll math -- tracking `scrollTop` and slicing rendered `[]string`.

---

## 7. Migration Order

Each step is a working commit with tests passing:

1. Define interfaces + split files (no behavior change).
2. Add ANSI utilities (`ansi.go`) -- pure functions, independently testable.
3. Add markdown renderer (`markdown.go`) -- string in, `[]string` out, independently testable.
4. Replace timeline rendering -- `ChatComponent` uses markdown for assistant entries.
5. Replace textarea with `EditorComponent` -- dynamic height, word wrap, history.
6. Add `SpinnerComponent` -- ticker, show/hide on InFlight.
7. Implement slash commands -- wire callbacks, implement `/cancel`, `/new`, `/resume`, `/model`.

---

## 8. Dependencies

### Added

| Package | Purpose |
|---|---|
| `github.com/yuin/goldmark` | Markdown parsing |
| `github.com/alecthomas/chroma/v2` | Syntax highlighting |
| `github.com/rivo/uniseg` | Grapheme clustering, East Asian width |

### Removed

- `github.com/charmbracelet/bubbles/textarea` (replaced by EditorComponent)
- `github.com/charmbracelet/bubbles/viewport` (replaced by ChatComponent scroll)

### Kept

- `github.com/charmbracelet/bubbletea` (event loop, terminal I/O)
- `github.com/charmbracelet/lipgloss` (ANSI styling helpers)

---

## 9. What Stays Unchanged

- `Frontend` interface and implementation.
- `ApprovalOverlay` logic (adapts to `Interactive` interface, same behavior).
- `progress.go` pure functions (used inside `StatusBarComponent`).
- Test infrastructure (`teatest`, golden files).
