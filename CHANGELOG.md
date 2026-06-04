# Changelog

## v0.2.0

Complete rewrite from web-based agent to a single-binary terminal UI (TUI) built with [Bubble Tea](https://github.com/charmbracelet/bubbletea). The old web server, SQLite session store, and browser frontend have been replaced by a Go-native TUI with JSONL-backed sessions.

### Added

**Core agent loop**
- Streaming LLM turns with tool dispatch and multi-turn conversation
- Error tiers with panic recovery
- Clean cancellation via context
- Sequential subagent spawning with isolated sessions

**LLM adapters**
- Anthropic-wire adapter (SSE streaming, tool use accumulation, usage extraction)
- OpenAI-compatible adapter (SSE streaming, tool_calls accumulation, usage extraction)
- Shared retry logic with configurable backoff for transient failures
- Client factory that selects adapter by endpoint wire format

**Tools**
- `read` -- read files with optional line range
- `write` -- create or overwrite files
- `edit` -- exact string replacement with read-before-edit enforcement
- `bash` -- shell command execution with context-aware cancellation
- `subagent` -- spawn isolated child agent sessions

**Guardrails and approval**
- Whitelist-based auto-approve with glob anchoring
- Four-option approval prompt: allow once, allow always, deny, deny + steer
- Editable suggested patterns for allow-always rules
- Per-project and global whitelist files with file-locked persistence
- `/yolo` toggle to skip all prompts

**TUI**
- Alt-screen Bubble Tea application with component architecture
- Goldmark-based ANSI markdown renderer with syntax highlighting
- ANSI-aware text utilities (visual width, truncation, word wrap)
- Editor component with dynamic height, word wrap, cursor navigation, and input history
- Chat component with markdown rendering and scrollback
- Braille spinner animation during LLM turns
- Session strip with focus cycling (`Ctrl+]` / `Ctrl+[`)
- Status bar showing model, endpoint, and yolo state
- Token progress bar with color thresholds at 80% and 95%
- Token usage detail overlay (`/tokens`)
- Tool call lines with expandable detail
- Tool execution status colors (pending / success / error)
- Context fullness warnings
- Cross-session notification line for subagent activity
- Slash command palette with inline autocomplete
- Bracketed paste support
- External editor integration (`Ctrl+E`, reads `$VISUAL` / `$EDITOR` / `vi`)

**Session persistence**
- JSONL-based session transcripts grouped by project
- Session list and resume (`--resume` flag loads most-recent session)
- Event types: session_start, session_end, user_message, assistant_turn, tool_call, tool_result, approval, subagent_spawn, subagent_done

**Configuration**
- Global settings at `~/.gohome/settings.json`
- Project-level overrides at `./.gohome/settings.json`
- Endpoint config with wire format, base URL, API key (literal or env var), model, and context window
- `--endpoint` and `--model` CLI flags for per-run overrides

**CLI**
- `--version` flag
- `--yolo` flag to start with approval prompts disabled
- `--resume` flag to continue the most recent session
- Structured JSON logging to `~/.gohome/logs/`
- Graceful shutdown with signal handling (SIGINT / SIGTERM)

**CI**
- Cross-platform test matrix (Linux, macOS, Windows)
- `go vet` and `golangci-lint` checks
- Cross-build for linux/amd64, darwin/arm64, darwin/amd64, windows/amd64
- Binary size guard (25 MB limit)
- Opt-in end-to-end smoke test
- Agent/TUI seam enforcement test

### Removed

- Web server (`internal/server`)
- Browser frontend (`web/static/`)
- SQLite session store (`internal/session`)
- MCP client (`internal/mcp`)
- Old agent loop and approval broker
- Goreleaser config and old CI/release workflows
- Dockerfile and Makefile

## v0.1.1

- File edit tool
- Documentation updates

## v0.1.0

- Initial release with web-based agent UI
- Anthropic LLM client with thinking blocks
- File read, file write, and shell tools
- SQLite-backed session persistence
- MCP client support
