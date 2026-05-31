# TUI Gap Analysis

Gap analysis comparing the Go TUI (`internal/tui`) against the reference TypeScript TUI
from the `pi` project (`packages/tui` + `packages/coding-agent/src/modes/interactive`).

---

## 1. Current Go TUI Inventory

### tui.go
Root Bubbletea model. Contains:
- `TimelineEntry` struct: kind, text, toolName, toolResult, expanded flag.
- `SessionView` struct: session state (timeline, usage, inflight, context-warning sentinels).
- `Model` struct: all root state -- sessions map, input textarea, viewport, window dimensions, approval state, token overlay flag, status message, YOLO flag/callback.
- `New()`: constructs model, wires shared input channel from Frontend.
- `Update()`: central dispatch -- `WindowSizeMsg`, `tea.KeyMsg` (Ctrl+C, Enter, slash commands, Up/Down/PgUp/PgDown, Ctrl+]/[, default to textarea), `agentEventMsg`, default passthrough.
- `handleAgentEvent()`: routes `EventTokenDelta`, `EventToolCallDone`, `EventToolResult`, `EventUsageUpdated`, `EventTurnDone`, `EventSessionStarted`, `EventSessionEnded`, `EventError` into `SessionView` mutations.
- `View()`: assembles session strip + viewport + notification line + status message + input region + status bar.
- `renderTimeline()`: plain-text timeline renderer with collapsed/expanded tool lines and cursor marker.
- `handleSlashCommand()`: implements `/quit`, `/yolo`, `/tokens`; others report "not implemented".
- `slashComplete()` and `slashPalette()`: prefix autocomplete list above input.
- `renderTokensOverlay()`: text table of token usage breakdown.
- Context-warning logic at 80%/95% thresholds.

### frontend.go
Bridges Bubbletea and the agent layer:
- `Frontend` struct: holds `*tea.Program` and `chan string` for user input.
- `Emit()`: non-blocking send of `AgentEventMsg` to the Bubbletea program.
- `RequestApproval()`: sends `ApprovalReqMsg` to Bubbletea loop, blocks on reply channel or ctx cancellation.
- `AwaitUserInput()`: blocks on input channel or ctx cancellation.
- Compile-time interface assertions for `agent.Frontend` and `guard.Frontend`.

### approval.go
Approval overlay state and rendering:
- `approvalPrompt` struct: req, reply channel, pattern string, edit sub-mode (patternInput textinput), steer sub-mode (steerInput textinput).
- `newApprovalPrompt()`: constructor.
- `bashCommand()`: extracts `command` field from bash tool input JSON.
- `renderApprovalOverlay()`: lipgloss rounded-border box showing tool name, options 1-4, sub-mode views.
- Key handling lives in `tui.go` (`handleApprovalKey`).

### progress.go
Context bar utility:
- `barCells()`: pure math -- given used/total/width, returns filled cell count (clamped).
- `progressBar()`: renders a block bar with green/yellow/red color thresholds at 50%/80%.

### statusbar.go
Bottom status bar:
- `formatTokens()`: formats integer as "12.3k" for values >= 1000.
- `usedTokens()`: sum of input+output tokens from Usage struct.
- `statusBar()`: renders one line: sessionID, modelName, progressBar, used/total (%), [YOLO].

### strip.go
Top session strip and focus navigation:
- `sessionStrip()`: renders "Session: " followed by chips (reversed for focused, normal for others) with running/done state label.
- `focusNext()` / `focusPrev()`: cycle focus with approval demotion/promotion.
- `demoteActiveApproval()` / `focusedIndex()`: helpers.

### style/style.go
Static theme:
- `Theme` struct: 5 lipgloss styles -- UserMsg, AssistantMsg, ToolLine, StatusBar, Notification.
- `Default()`: fixed terminal 256-color palette (colors 12, 7, 3, 8/0, 11).
- No hot-reload, no color downsampling, no truecolor support.

---

## 2. Architecture Comparison

### Rendering Approach
- **Reference:** Custom retained-mode component tree. Line-level differential rendering -- compares `[]string` between frames, rewrites only changed lines. Synchronized output (`\x1b[?2026h`) prevents flicker. Rate-limited to ~16ms minimum.
- **Go TUI:** Bubbletea's `tea.Program` with `tea.WithAltScreen()`. `View()` returns a single string; Bubbletea handles diffing internally. No custom diff algorithm.

### Component Model
- **Reference:** Explicit component interface (`render`, `handleInput`, `invalidate`). Components compose via `Container` children lists. Overlays composited at character level. Render caches per-component keyed on inputs.
- **Go TUI:** No component interface. All rendering is done via methods on the single `Model` struct. `statusBar()`, `sessionStrip()`, `renderApprovalOverlay()`, etc. are functions returning strings, not composable components. No render caching, no invalidation, no child tree.

### Input Handling
- **Reference:** Raw stdin bytes -> StdinBuffer (sequence assembler, 10ms timeout) -> TUI.handleInput() -> inputListeners chain -> focusedComponent. Handles CSI, OSC, bracketed paste, Kitty protocol, SGR mouse, high-byte meta encoding.
- **Go TUI:** Bubbletea handles stdin parsing entirely. Key events arrive as `tea.KeyMsg`. No custom sequence assembler, no bracketed paste, no Kitty protocol.

### Keybinding System
- **Reference:** Dot-namespaced binding IDs, multiple `KeyId` values per binding, user overrides, conflict detection.
- **Go TUI:** No keybinding system. Keys are matched inline via `switch msg.Type` and `keyRune(msg)` comparisons. Hardcoded throughout `Update()` and `handleApprovalKey()`. No user customization.

### Theming
- **Reference:** 54 fg + 6 bg colors, named semantically, truecolor with 256-color fallback, hot-reload, global singleton.
- **Go TUI:** 5 lipgloss styles, hardcoded 256-color palette values, no hot-reload, no user customization.

---

## 3. Feature Gap Matrix

| Feature | Reference TUI | Go TUI | Gap |
|---|---|---|---|
| **Rendering engine** | | | |
| Line-level diff rendering | Custom `previousLines[]` compare | Bubbletea handles internally | Delegated; acceptable |
| Synchronized output | `\x1b[?2026h`/`l` | Not explicitly controlled | Missing |
| Rate limiting / 16ms cap | `nextTick` coalescing + 16ms | Bubbletea renders on every message | No control |
| Render caching per component | Per-component cache keyed on inputs | None | Missing |
| **Component model** | | | |
| Component interface | `render`, `handleInput`, `invalidate` | None -- monolithic Model | Missing entirely |
| Container children list | Yes | No | Missing |
| Overlay compositing (character-level) | `compositeLineAt()` | No | Missing entirely |
| Render cache invalidation | `invalidate()` propagates | No | Missing |
| Box (padding container with bg fill) | Yes | No | Missing |
| **Terminal abstraction** | | | |
| Bracketed paste mode | Yes -- separate paste events | No | Missing |
| Kitty keyboard protocol | Yes -- negotiation with fallback | No | Missing |
| **Input handling** | | | |
| Bracketed paste event | Strips markers, emits paste event | No | Missing |
| Kitty CSI-u decoding | Yes | No | Missing |
| Non-Latin keyboard (baseLayoutKey) | Yes | No | Missing |
| **Keybinding system** | | | |
| Named binding IDs | Dot-namespaced | Hardcoded switch statements | Missing |
| Multiple keys per binding | Yes | No | Missing |
| User customization (overrides) | Yes | No | Missing |
| Conflict detection | Yes | No | Missing |
| **Text editing -- single line (Input)** | | | |
| Single-line input with cursor | `Input` component, grapheme-aware | Bubbletea textarea (multi-line) | Uses textarea |
| Horizontal scroll | Yes | Textarea wraps instead | Different behavior |
| Grapheme-aware cursor movement | `Intl.Segmenter` | Bubbletea handles basics | Missing for complex Unicode |
| Bracketed paste normalization | Strips newlines, normalizes tabs | No | Missing |
| History (up/down cycles past inputs) | 100-entry history | No | Missing |
| **Text editing -- multi-line (Editor)** | | | |
| Multi-line editor with word wrap | Full `Editor` component | Fixed 3-line textarea | Very limited |
| Vertical scroll in editor | Yes | No -- always 3 rows | Missing |
| Dynamic height (30% of terminal) | Yes | No -- always 3 rows | Missing |
| Sticky column tracking | 7-case decision table | No | Missing |
| Paste collapse markers | Large pastes collapsed to `[paste #N]` | No | Missing |
| Character jump mode | Yes | No | Missing |
| External editor support (`$EDITOR`) | Yes | No | Missing |
| **Emacs editing** | | | |
| Kill ring | `[]string` stack, accumulate, rotate | No | Missing entirely |
| Undo/redo | Coalescing undo stack | No | Missing |
| Yank / yank-pop | Yes | No | Missing |
| **Text display** | | | |
| Word wrapping with ANSI preservation | `wrapTextWithAnsi`, AnsiCodeTracker | Plain string concat, no wrapping | Missing |
| ANSI-aware width calculation | `sliceByColumn`, grapheme widths | No | Missing |
| ANSI-aware text truncation | `truncateToWidth()` | Naive byte slicing `s[:57]` | Buggy |
| **Markdown rendering** | | | |
| Markdown rendering | Full `marked`-based renderer | Plain text only | Missing entirely |
| Headings, code blocks, tables | Yes | No | Missing |
| Syntax highlighting | Yes | No | Missing |
| OSC 8 clickable hyperlinks | Yes | No | Missing |
| Thinking block collapse | Yes | No | Missing |
| **List components** | | | |
| SelectList (scrollable, filterable) | Two-column, scroll indicator | No | Missing entirely |
| SettingsList (key/value, cycling) | Fuzzy filter, submenu delegation | No | Missing |
| **Overlay system** | | | |
| Overlay stack (z-ordered) | `overlayStack[]`, `focusOrder` | One approval overlay only | Missing |
| Modal/non-modal overlays | Capturing vs non-capturing | Always modal | Missing |
| Arbitrary positioning | 9 anchors, margin, percentage sizes | Approval box is full-width | Missing |
| Focus save/restore | `preFocus` snapshot | Very basic | Missing |
| Nested overlays | Yes | No | Missing |
| **Selectors** | | | |
| Session selector | Tree, fuzzy, delete, rename | No session browser | Missing entirely |
| Model selector | Yes | /model is stubbed | Missing |
| Config selector | Yes | No | Missing |
| Settings selector | Yes | No | Missing |
| Theme selector with live preview | Yes | No | Missing |
| Tree selector (conversation tree) | Yes | No | Missing |
| **Theming** | | | |
| Named semantic colors (54+6) | Yes | 5 styles only | Severely limited |
| Truecolor support | Hex colors | 256-color only | Missing |
| Hot-reload custom themes | File watcher, 100ms debounce | No | Missing |
| User-customizable theme file | Yes | No | Missing |
| **Chat layout** | | | |
| Named container layout | 7 named containers | Monolithic string concat in View() | Missing |
| User message with background color | `userMessageBg` Box | Plain "you: " prefix | Missing |
| Tool execution color states | 3 bg states (pending/success/error) | Single collapsed line | Very limited |
| Pending messages queue display | Yes | No | Missing |
| **Footer/status bar** | | | |
| Branch, cwd display | `~/path (branch)` | Session ID only | Missing |
| Token breakdown (cache read/write, cost) | Yes | Input+Output only | Partial |
| Model name + thinking level | Yes | Model name only | Partial |
| Auto-compaction indicator | Yes | No | Missing |
| **Autocomplete** | | | |
| Slash command autocomplete | Fuzzy, per-command arg completion | Prefix match only | Partial |
| File path completion | Tab-triggered, dirs first | No | Missing |
| `@`-prefix fuzzy file search | `fd` subprocess, scored | No | Missing |
| Fuzzy matching algorithm | Sequential match, scored | Prefix only | Missing |
| **Image support** | | | |
| Kitty/iTerm2 inline images | Yes | No | Missing entirely |
| Clipboard image paste | Yes | No | Missing |
| **Loader/spinner** | | | |
| Animated braille spinner | 80ms interval | No | Missing entirely |
| CancellableLoader | AbortSignal support | No | Missing |
| **Session management UI** | | | |
| Session browser/selector | Full tree selector | No | Missing |
| /new, /resume implementations | Yes | Stub -- "not implemented" | Missing |
| Fork session | Yes | No | Missing |

---

## 4. What the Go TUI Does Well

1. **Clean architecture separation.** The `agent.Frontend` interface is clearly defined with compile-time assertions. The TUI does not import the agent implementation -- only the interface.

2. **Approval system is well-tested and functional.** Covers all four decisions (allow once, allow always, deny, deny+steer), editable patterns, Esc-to-cancel from sub-modes, and cross-session queueing. 14 tests in `approval_test.go`.

3. **Context warning system works correctly.** The 80%/95% threshold warnings, once-per-session sentinel flags, and notification line display are implemented and tested.

4. **Token usage overlay.** The `/tokens` overlay shows a full breakdown including cache reads and writes, which is more detailed than the reference footer.

5. **Session focus cycling is solid.** Ctrl+]/[ cycling with approval demotion/promotion is a clean implementation.

6. **Good test infrastructure.** Uses `teatest` for live integration tests and `golden` package for snapshot tests. `TestMain` forces `termenv.Ascii` for deterministic snapshots.

7. **Bubbletea's viewport gives explicit scrollback.** PgUp/PgDn scrollback inside the TUI, which the reference does not have (it relies on terminal scrollback only).

8. **Progress bar is reusable and well-tested.** `barCells()` and `progressBar()` are pure functions with thorough table-driven tests.

9. **Structured logging.** JSON slog to `~/.gohome/logs/YYYY-MM-DD.log` with debug toggle via `GOHOME_DEBUG=1`.

---

## 5. Priority Gaps

### P0 -- Blocking (cannot have a usable TUI without these)

1. **Markdown rendering.** Assistant output currently shows raw markdown syntax as plain text. Need a goldmark-based ANSI renderer or `charmbracelet/glamour`.

2. **Multi-line editor that works properly.** Current textarea is fixed at 3 rows. Need dynamic height (up to ~30% of terminal), word wrapping, and vertical scroll.

3. **Slash command implementations.** `/new`, `/resume`, `/model`, `/cancel` all print "not implemented". These are essential for daily use.

4. **Animated spinner for in-flight state.** No visual indication that the agent is processing beyond "running" text in the session strip chip.

5. **Input history.** No Up/Down cycling through past prompts. Need a ring buffer (up to 100 entries).

### P1 -- Important (significantly degrades experience)

6. **ANSI-aware text truncation.** `shortSummary()` does naive byte slicing (`s[:57]`), corrupts display with ANSI/multi-byte content.

7. **Tool execution rich display.** Tool calls render as a single collapsed line. Need background color states (pending/success/error) and expandable full content.

8. **Bracketed paste support.** Without it, pasting multi-line content submits multiple times.

9. **Word wrapping of assistant messages.** Long lines from the LLM overflow or truncate.

10. **External editor support.** `$EDITOR`/`$VISUAL` launch for writing substantial prompts.

11. **Consistent context bar thresholds.** Progress bar colors at 50%/80% but warnings fire at 80%/95%.

### P2 -- Nice-to-have (polish and power-user features)

12. Fuzzy autocomplete for slash commands.
13. File path completion (`@file`, Tab).
14. Kill ring / Emacs editing (Ctrl+K, Ctrl+Y).
15. Truecolor / semantic theme system with hot-reload.
16. Session browser UI.
17. Model selector.
18. Loader as a proper component.
19. Image support (Kitty/iTerm2).
20. Thinking block display.
21. Pending message queue (Alt+Enter while streaming).
22. Keybinding system with user customization.
23. OSC 133 shell integration markers.
24. Overlay system with z-ordering and focus management.
