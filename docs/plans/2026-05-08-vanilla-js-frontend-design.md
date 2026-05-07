# Vanilla JS Frontend Redesign

## Goal

Remove npm/npx from the project entirely to eliminate the npm build pipeline as a supply chain attack surface. Rewrite the frontend in plain HTML + JS with no framework, no build step, and no external dependencies.

## File Structure

### Deleted
- `web/package.json`
- `web/package-lock.json`
- `web/node_modules/`
- `web/src/app.tsx`
- `web/src/types.ts`
- `web/src/components/ChatView.tsx`
- `web/src/components/Sidebar.tsx`
- `web/src/components/ToolCallBlock.tsx`

### Added
- `web/static/index.html` — markup, loads `app.js` as `type="module"`
- `web/static/app.js` — all app logic in one JS file
- `web/static/app.css` — moved from `web/src/app.css`, content unchanged

### Updated
- `embed.go` — embed `web/static/` instead of `web/dist/`
- `Makefile` — remove `frontend` target; `build` just runs `go build`; `clean` removes nothing npm-related

The Go binary continues to serve everything as an embedded filesystem.

## State and Rendering

State is a plain object:

```js
const state = {
  sessions: [],
  activeSessionId: null,
  messages: [],
  busy: false,
  awaitingApproval: null,
};
```

Rendering is split by update frequency:

- **Infrequent** (sessions list, full message history): rebuild DOM subtree via innerHTML.
- **High-frequency** (streaming tokens): append text directly to a persistent streaming bubble element. The streaming bubble is created when streaming starts and promoted to a real message on `done`, avoiding per-token re-renders.

The `ToolCallBlock` expand/collapse uses a CSS class toggle and `data-expanded` attribute — no JS state needed.

## WebSocket and App Logic

Same connection behavior as the current Preact app: exponential backoff reconnect, tab ID via `crypto.randomUUID()`, same message protocol.

### Incoming message handlers

| Message | Handler |
|---|---|
| `token` | `appendToken(data)` — appends to streaming bubble |
| `sessions` | `renderSessions(data)` — rebuilds sidebar |
| `history` | `renderMessages(messages)` — rebuilds message list, clears stream |
| `tool_approval` | `showApprovalModal(req)` — shows modal |
| `tool_result` | `addMessage(syntheticMsg)` — appends tool result row |
| `done` | `finalizeStream(messageId)` — promotes streaming bubble to message |
| `stopped` | `clearStream()` — clears streaming bubble |
| `error` | `showError(text)` — shows error banner |

### Outgoing messages

All sent via a single `send(obj)` helper: `message`, `tool_response`, `stop`, `new_session`, `load_session`, `delete_session`.

UI events (approval modal, stop button, send form) wired via `addEventListener`.
