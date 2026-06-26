# Changelog

## v0.2.5

### Added

- **Subagent shadow entries** -- Child subagent tool calls are mirrored as indented, dimmed entries in the parent timeline, providing visibility into subagent activity without switching focus. A sliding window of max 3 shadows per child is maintained.
- **Tool output preview** -- Collapsed tool entries now show the last 3 lines of output as dimmed preview text, giving a quick glance at results without expanding.
- **Edit tool diff box** -- Edit tool calls render a bordered unified diff preview below the entry, with red/green line coloring for removals/additions. Error status shows a red border with dimmed content.
- **Subagent read-only mode** -- Completed subagent sessions now display a "[Session complete]" label and reject text input, preventing accidental interaction with finished sessions.
- **Cursor-viewport sync** -- Arrow key navigation through timeline entries now scrolls the viewport to keep the cursor on-screen, with pinning for entries taller than the viewport.

### Fixed

- **Subagent sessions stuck in "running" state** -- `EventSessionEnded` is now emitted when a subagent finishes or is cancelled, so the TUI correctly clears the InFlight flag (#16).
- **Subagent lifecycle state tracking** -- Subagent sessions now show accurate states: "running" while InFlight, "returning" when returning results to the parent, and "complete" when the parent receives the tool result (#18).
- **Scroll flicker on tall entries** -- `EnsureCursorVisible` no longer oscillates between top and bottom when an entry is taller than the viewport.
- **PgUp/PgDn scroll amount** -- Page scrolling now uses full-page increments instead of a fixed 5-line jump.
- **WrapText newline handling** -- Fixed line counting for content containing embedded newlines, correcting scroll math for multi-line tool output.

### Changed

- **LLM client scaffold deduplication** -- Extracted shared HTTP retry, error handling, and SSE pump logic into `common.StreamRequest`, eliminating ~165 lines of duplicated code between the Anthropic and OpenAI adapters. Deleted per-adapter `retry.go` files (M1).

## v0.2.4

### Fixed

- **Mutex self-deadlock in session swaps** -- `/new` and `/resume` slash command closures no longer deadlock by re-acquiring a held lock; closures now receive `(oldSess, oldWriter)` as parameters.
- **Data race in model switching** -- `SetModel` callback no longer writes `sess.ModelConfig` outside the lock; new `SetModelConfig()` method provides proper synchronization.
- **Stale model state on `/new`** -- `/new` now reads the current model config at call time instead of capturing the startup value.
- **Crash on `/new` after `/resume`** -- `runLoop` held stale session/writer references across the blocking `AwaitUserInput` call; a session swap closed the old writer, and sending on the closed channel panicked. References are now re-fetched after unblocking.

### Changed

- **Active modal system** -- Replaced the cascading flag-based modal system with an `activeModal` slot implementing the `Interactive` interface. New `HelpOverlay` and `TokensOverlay` types reduce `Update()` key dispatch from a 6-level priority ladder to a 3-level cascade (H4 completion).
- **Consolidated `/endpoint` and `/model`** -- Merged the half-implemented `/endpoint` and `/model` commands into a single `/model` command that rebuilds the LLM client at runtime. Config terminology renamed from `Endpoint` to `ModelConfig` throughout (M2).
- **File reorganization** -- Renamed `model_overlays.go` to `model_external_editor.go` (now contains only external editor logic).

## v0.2.3

### Added

- **Render caching** -- Per-entry render cache eliminates redundant markdown and syntax highlighting on unchanged timeline entries, dramatically improving TUI responsiveness in large conversations.
- **Render throttling** -- New optional `renderThrottleMs` config setting gates redraws during token streaming, preventing excessive refreshes on slow terminals. Defaults to 0 (no throttling).
- **Editor word wrap** -- Long lines in the input editor now wrap at word boundaries with sticky column navigation.
- **CLAUDE.md developer guide** -- Comprehensive documentation of project architecture, build/test commands, key interfaces, data flow, CI setup, and debug logging.
- **Code audit (FABLE_REVIEW.md)** -- Detailed technical review documenting high, medium, and low priority findings with line-number citations and suggested fixes.

### Fixed

- **Turn cancellation wired end-to-end** -- `/cancel`, Esc, and single Ctrl+C now properly cancel in-flight LLM turns via per-turn context cancellation, preventing UI/agent state divergence (H1).
- **Data races in session swaps** -- `/new` and `/resume` are now safe to call mid-turn. A new mutex-guarded `agent.State` type owns session and writer references, and swaps cancel the active turn first (H2).
- **OpenAI adapter parallel tool results** -- Multiple tool results in a single turn are no longer silently dropped. `buildOpenAIBody` now flattens `RoleTool` messages into one `openaiToolMessage` per `BlockToolResult` block (H3).

### Changed

- **TUI model decomposition** -- Split `model.go` (1407 lines) into focused subfiles: `model_agent.go`, `model_approval.go`, `model_keys.go`, `model_slash.go`, `model_overlays.go` (partial H4).
- **Main loop refactored** -- New `runLoop()` encapsulates the agent REPL with per-turn context cancellation, cleanly separating the event loop from session lifecycle management.

## v0.2.2

### Added

- **Help overlay** -- `/help` command and `Ctrl+H` keybinding open a scrollable overlay showing all keybindings, slash commands, and CLI flags.
- **Configurable settings** -- Context window, max tokens, thinking budget, bash timeouts, context warning thresholds, and retry backoff are now configurable via `settings.json` instead of being hardcoded. All new fields use `omitempty` so existing configs require no changes.
- **Copy to clipboard** -- Press `c` while browsing history to copy the selected entry to the system clipboard. ( Does not work over SSH )
- **Scroll-stable block expansion** -- Expanding thinking or tool blocks no longer jumps the viewport. The scroll position is anchored and expanded blocks get a subtle background highlight.
- **Thinking blocks in OpenAI wire protocol** -- The OpenAI-compatible adapter now parses `reasoning_content` deltas and emits thinking events, enabling thinking block rendering for OpenAI-wire endpoints.
- **Thinking block persistence** -- Thinking blocks (including signature data) are saved to session JSONL files so they are preserved and displayed when resuming a session.
- **Session load validation** -- Malformed thinking blocks (empty text) are logged as warnings when loading session files, providing visibility into data quality without blocking resume.

### Fixed

- Thinking block signature field is now captured from Anthropic `signature_delta` events and forwarded through the stream, fixing extended thinking continuations on session resume.
- LLM adapters now handle `BlockThinking` when building request messages for resumed sessions, preventing dropped thinking blocks.
- The `c` key no longer swallows input when the timeline is empty.
- Context warning thresholds are validated at startup to ensure the warning level is below the critical level.
- Thinking blocks now collapse automatically after reasoning completes, matching the collapsed-by-default behavior of resumed sessions.
- Resumed thinking blocks default to collapsed (previously expanded), reducing visual noise when loading old sessions.

## v0.2.1

### Added

- **Thinking blocks** -- Anthropic thinking/reasoning blocks are parsed from the SSE stream, forwarded through the agent event pipeline, and rendered in the TUI as collapsible entries with line counts. Spinner shows "Thinking..." during reasoning and "Generating..." during token output.
- **File search popup** -- Type `@` followed by a query to search project files using `fd` (with `find` fallback). Results are scored and ranked (exact filename > prefix > substring). Navigate with Up/Down or Tab, confirm with Enter to insert the path.
- **Pending message queue** -- Messages typed while the agent is streaming are queued and automatically sent when the current turn completes. Queue is cleared on `/cancel`.
- **Ctrl+C cancellation** -- Ctrl+C cancels in-flight turns or dismisses approval prompts; double-tap quits the app. Escape also cancels the spinner.
- **Table rendering** -- GFM tables are rendered with box-drawing borders in markdown output.
- **Tab completion for slash commands** -- Tab auto-completes slash commands with first-match highlighting in the palette.
- **Session browser** -- `/resume` now shows an interactive session browser with filtering and search via a new SelectListComponent.
- **Model selector** -- New `/model` command with interactive ModelSelectorComponent for switching LLM models at runtime.
- **Resume with history** -- When resuming a session, prior conversation history is loaded and displayed in the TUI.
- **Approval prompt navigation** -- Up/Down arrows navigate approval options with selection markers, Enter confirms. Press `e` to edit allow-always patterns, `4` for deny+steer with steering message input.
- **Blank session cleanup** -- Empty JSONL session files (no user messages) are automatically removed on shutdown.
- **Token usage overlay** -- `/tokens` overlay shows token usage breakdown and context window percentage.
- **Context fullness warnings** -- Warnings displayed at 80% and 95% context window thresholds.
- **Tool status colors** -- Tool execution results shown with pending/success/error color indicators.
- **Status bar** -- Session ID, model name, token progress bar, and YOLO mode indicator.

### Fixed

- CI release workflow now creates draft releases and handles pre-existing releases correctly.
- Anthropic API requests now include the `thinking` parameter so thinking blocks are actually returned.

## v0.2.0

Complete rewrite from web-based agent to a single-binary terminal UI (TUI) built with [Bubble Tea](https://github.com/charmbracelet/bubbletea). The old web server, SQLite session store, and browser frontend have been removed.

### Added

- **TUI** -- Bubble Tea app with markdown rendering, syntax highlighting, input editor with history, token progress bar, slash command palette, bracketed paste, and external editor support (`Ctrl+E`)
- **Dual LLM adapters** -- Anthropic-wire and OpenAI-compatible SSE streaming with shared retry logic
- **Tool-call guardrails** -- whitelist-based auto-approve, four-option approval prompt (allow once / allow always / deny / deny + steer), `/yolo` toggle
- **Subagents** -- spawn isolated child agent sessions from within the agent loop, with per-session TUI strips and focus cycling
- **JSONL sessions** -- file-based session persistence with `--resume` to continue the most recent session
- **Config** -- global (`~/.gohome/settings.json`) and project-level (`./.gohome/settings.json`) endpoint configuration
- **CLI flags** -- `--version`, `--endpoint`, `--model`, `--yolo`, `--resume`
- **CI** -- cross-platform test matrix, lint, cross-build (linux/amd64, darwin/arm64, darwin/amd64, windows/amd64), binary size guard

### Removed

- Web server, browser frontend, SQLite session store, MCP client, Goreleaser config, Dockerfile

## v0.1.1

- File edit tool
- Documentation updates

## v0.1.0

- Initial release with web-based agent UI
- Anthropic LLM client with thinking blocks
- File read, file write, and shell tools
- SQLite-backed session persistence
- MCP client support
