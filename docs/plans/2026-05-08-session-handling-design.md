# Session Handling Improvement Design

## Problem

On every fresh page load, the frontend immediately creates a blank session in the database. If the user never types anything, this empty session persists across restarts. Additionally, all sessions share the default title "New Session" with no way to distinguish them.

## Goals

1. No session is created until the user sends their first message.
2. Each session gets a short auto-generated title (max 10 words) from the LLM, derived from the first user message.
3. Fresh page load shows an empty state with the session list populated in the sidebar.

## Architecture

On page load, the frontend sends `{ type: "list_sessions" }` to populate the sidebar. No session is created. `activeSessionId` stays `null` and the chat area shows an empty state.

When the user sends their first message, the frontend sends `{ type: "message", session_id: "", content: "..." }`. The backend detects the empty `session_id`, creates a session, and immediately sends back `{ type: "session_created", session_id: "..." }` plus an updated `sessions` list. The agent then runs normally. After the agent run completes, a goroutine calls `GenerateTitle` with the first message, updates the session title in the DB, and broadcasts another `sessions` update.

All subsequent messages use the now-set `activeSessionId`. Reconnection behavior is unchanged: if `activeSessionId` is set, `ws.onopen` sends `load_session`.

## Components

### `internal/session/store.go`
- Add `UpdateSessionTitle(ctx context.Context, id, title string) error`

### `internal/agent/loop.go`
- Add `GenerateTitle(ctx context.Context, message string) (string, error)`
- Calls `llm.Complete` with a minimal system prompt: generate a title of at most 10 words for a conversation starting with the given message
- Returns the trimmed response content

### `internal/server/server.go`
- Add `list_sessions` WS message handler: responds with the current sessions list
- `message` handler: if `session_id == ""`, create a session first, send `session_created` + `sessions`, then run the agent
- After agent run on a new session: spawn goroutine with 10s timeout → `GenerateTitle` → `UpdateSessionTitle` → broadcast `sessions`
- `outMsg` already has a `SessionID` field; use it for `session_created`

### `web/static/app.js`
- `ws.onopen`: send `{ type: "list_sessions" }` instead of `new_session`
- Handle `session_created`: set `activeSessionId = msg.session_id`, re-render session list
- Show empty state in chat area when `activeSessionId === null`

## Data Flow

```
Fresh page load:
  WS open → list_sessions → sessions (sidebar populated, chat empty)

First message:
  user sends → { type: "message", session_id: "", content: "..." }
  backend creates session → session_created + sessions
  frontend sets activeSessionId, highlights new session in sidebar
  agent runs → tokens / tool approvals / done
  goroutine: GenerateTitle → UpdateSessionTitle → sessions broadcast
  sidebar title updates: "New Session" → generated title
```

## Error Handling

- `GenerateTitle` failure: log error, leave title as "New Session". Never blocks main flow.
- `GenerateTitle` goroutine runs with a 10-second context timeout.
- Session creation failure on first message: send `error` to frontend.
