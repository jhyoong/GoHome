# Changelog

## v0.1.1 (2026-05-16)

### Features
- **Thinking blocks**: Model reasoning is now displayed when `thinking_tokens` is configured. Thinking blocks are always visible and collapsible in the chat UI.
- **Context window tracker**: Token usage and context window percentage are shown below the input bar, updating in real time as the conversation grows.
- **Dark mode**: Toggle between light and dark themes via the sidebar button. Preference persists across sessions via localStorage.
- **Multi-line input**: The input field is now a textarea that auto-resizes up to 5 lines. Use Shift+Enter to insert a newline; Enter submits.

### Bug Fixes
- Fixed division by zero when context window size is zero in usage calculation.
- Fixed a race condition where context window data could be read while being updated.
- Fixed textarea not resizing correctly on initial render.
- Fixed theme colors not applying to the input field background and text.

---

## v0.1.0 (2026-05-08)

Initial release.

- Single Go binary with embedded vanilla JS frontend (no build step)
- Connects to any OpenAI-compatible local LLM endpoint
- Tool execution with per-tool approval: shell, file_read, file_write
- Approval whitelist: always allow, never allow, or ask per tool/pattern
- Always-allow persists to config on disk
- MCP server support (stdio and SSE transports)
- Session persistence in SQLite
- Multi-session sidebar
- Streaming token output via WebSocket
