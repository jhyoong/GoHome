# Changelog

## v0.2.1

### Added

- **Thinking blocks** -- Anthropic thinking/reasoning blocks are parsed from the SSE stream, forwarded through the agent event pipeline, and rendered in the TUI as collapsible entries with line counts. Spinner shows "Thinking..." during reasoning and "Generating..." during token output.
- **File search popup** -- Type `@` followed by a query to search project files using `fd` (with `find` fallback). Results are scored and ranked (exact filename > prefix > substring). Navigate with Up/Down, confirm with Enter to insert the path.
- **Pending message queue** -- Messages typed while the agent is streaming are queued and automatically sent when the current turn completes. Queue is cleared on `/cancel`.

### Fixed

- CI release workflow now creates draft releases and handles pre-existing releases correctly.

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
