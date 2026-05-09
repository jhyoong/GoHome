# Implementation Plan: Thinking Blocks

## Index

| ID   | Title | Status |
|------|-------|--------|
| T001 | Add thinking field to Message data model and SQLite schema | [x] |
| T002 | Add thinking_tokens configuration to EndpointConfig | [x] |
| T003 | Update LLM client to parse predictions/thinking from OpenAI API responses | [x] |
| T004 | Add thinking token streaming support to WebSocket protocol | [ ] |
| T005 | Update frontend to render thinking blocks with toggle UI | [ ] |
| T006 | Add CSS styles for thinking blocks | [ ] |

---

## T001: Add thinking field to Message data model and SQLite schema

### Objective
Add a `Thinking` field to the `Message` struct in `internal/session/store.go` and update the SQLite schema to store thinking content separately from regular content.

### Scope files
- Allowed: `internal/session/store.go`
- Avoid touching: `internal/llm/client.go`, `internal/server/server.go`, `web/static/`

### Testing Strategy
- Test file location: `internal/session/store_test.go` (create if not exists)
- Test framework: Go test (standard `testing` package)
- Behaviors to assert:
  - `Message` struct given `Thinking: "some thinking content"` should serialize to JSON with `thinking` field
  - `AddMessage` with `Thinking` populated should insert row with thinking column
  - `GetMessages` should return messages with thinking content intact
  - SQLite schema should include `thinking` column

### Read hints
- Grep queries: `type Message struct`, `AddMessage`, `GetMessages`, `create table messages`
- Key entrypoints / symbols: `Message` struct, `ToolResult` struct, SQLite table creation

### Details
Update the `Message` struct to include:
```go
type Message struct {
    // existing fields...
    Thinking string `json:"thinking,omitempty"`
}
```
Add the `thinking` column to SQLite schema in store.go. Ensure backward compatibility with existing messages (nullable column).

### Acceptance checks
- Behavioral checks: Message struct has Thinking field; SQLite has thinking column
- Commands to run: `go test ./internal/session/...`

### Context budget
- Expected excerpts to read: Message struct, table creation SQL, insert/query SQL

---

## T002: Add thinking_tokens configuration to EndpointConfig

### Objective
Add a `ThinkingTokens` field to `EndpointConfig` in `internal/config/config.go` so users can configure max thinking token output per model.

### Scope files
- Allowed: `internal/config/config.go`
- Avoid touching: `internal/session/store.go`, `internal/llm/client.go`

### Testing Strategy
- Test file location: `internal/config/config_test.go` (create if not exists)
- Test framework: Go test
- Behaviors to assert:
  - `EndpointConfig` struct given `ThinkingTokens: 1024` should serialize to YAML with `thinking_tokens` field
  - Config loading should parse `thinking_tokens` from YAML
  - Default value should be sensible (e.g., 0 or omitted means model default)

### Read hints
- Grep queries: `type EndpointConfig struct`, `yaml:", inline"`
- Key entrypoints / symbols: `EndpointConfig` struct, config parsing

### Details
Add to `EndpointConfig`:
```go
type EndpointConfig struct {
    // existing fields...
    ThinkingTokens int `yaml:"thinking_tokens"`
}
```
Update YAML tags and documentation if any.

### Acceptance checks
- Behavioral checks: Config struct has ThinkingTokens field; YAML parsing works
- Commands to run: `go test ./internal/config/...`

### Context budget
- Expected excerpts to read: EndpointConfig struct, config parsing logic

---

## T003: Update LLM client to parse predictions/thinking from OpenAI API responses

### Objective
Update `internal/llm/client.go` to parse `predictions` content blocks from OpenAI API streaming responses and handle them separately.

### Scope files
- Allowed: `internal/llm/client.go`
- Avoid touching: `internal/server/server.go`, `internal/session/store.go`

### Testing Strategy
- Test file location: `internal/llm/client_test.go` (create if not exists)
- Test framework: Go test
- Behaviors to assert:
  - `StreamResponse` given SSE with `predictions` content block should call `handlePredictions` callback
  - `Delta` struct needs `Predictions` field; parsing should handle `predictions` content block
  - Prediction tokens should be streamed separately via new callback/field

### Read hints
- Grep queries: `Delta` struct, `StreamResponse`, `parseSSE`, `handleContent`
- Key entrypoints / symbols: `reqBody` struct, `streamOptions`, SSE parsing loop

### Details
OpenAI API sends `predictions` content blocks for models that support thinking. Need to:
1. Add `Predictions` field to `Delta` struct (or similar)
2. Add parsing logic for `predictions` content block in SSE parsing
3. Create callback or field to handle prediction tokens separately
4. Pass prediction tokens through the agent loop

### Acceptance checks
- Behavioral checks: LLM client parses predictions; streaming delivers prediction tokens
- Commands to run: `go test ./internal/llm/...`

### Context budget
- Expected excerpts to read: Delta struct, SSE parsing logic, stream handling

---

## T004: Add thinking token streaming support to WebSocket protocol

### Objective
Update `internal/server/server.go` to add a new `thinking_token` message type for streaming thinking content to frontend via WebSocket.

### Scope files
- Allowed: `internal/server/server.go`, `internal/server/dispatcher.go`
- Avoid touching: `web/static/app.js`, `internal/llm/client.go`

### Testing Strategy
- Test file location: `internal/server/server_test.go` (create if not exists)
- Test framework: Go test
- Behaviors to assert:
  - `outMsg` struct supports `thinking_token` type with token data
  - `dispatcher` sends `thinking_token` messages to WebSocket client
  - Frontend can receive `thinking_token` messages and parse token data

### Read hints
- Grep queries: `type outMsg struct`, `outMsg` json tags, `dispatchMessage`, `wsOp`
- Key entrypoints / symbols: `outMsg` struct, `wsOp` struct, dispatcher send logic

### Details
Add new message type to `outMsg`:
```go
type outMsg struct {
    Type string `json:"type"`
    Data any    `json:"data,omitempty"`
    // existing types: token, tool_approval, tool_result, done, error, usage
    // Add: thinking_token for streaming thinking content
}
```
Update `wsOp` struct similarly. Update dispatcher to handle thinking token streaming.

### Acceptance checks
- Behavioral checks: outMsg supports thinking_token type; dispatcher handles it
- Commands to run: `go test ./internal/server/...`

### Context budget
- Expected excerpts to read: outMsg struct, wsOp struct, dispatcher logic

---

## T005: Update frontend to render thinking blocks with toggle UI

### Objective
Update `web/static/app.js` to receive thinking tokens and render them as collapsible thinking blocks with a toggle button.

### Scope files
- Allowed: `web/static/app.js`
- Avoid touching: `internal/server/server.go`, `internal/session/store.go`

### Testing Strategy
- Test file location: `web/static/test_thinking_test.js` (create if not exists)
- Test framework: Jest (if available) or manual testing
- Behaviors to assert:
  - `handleThinkingToken` receives token and appends to thinking display
  - Thinking blocks are collapsible with toggle behavior
  - `appendToken` handles thinking tokens separately from content tokens
  - Message rendering includes thinking blocks when present

### Read hints
- Grep queries: `appendToken`, `msgHtml`, `toolCallBlockHtml`, `handleMessage`
- Key entrypoints / symbols: `appendToken()`, `msgHtml()`, message rendering functions

### Details
1. Add handler for `thinking_token` WebSocket messages
2. Add state tracking for thinking content visibility
3. Add toggle button mechanism to show/hide thinking blocks
4. Update `appendToken()` to handle thinking tokens separately
5. Update `msgHtml()` to include thinking blocks in rendered message

### Acceptance checks
- Behavioral checks: Thinking blocks render; toggle shows/hides thinking
- Commands to run: (manual testing via browser)

### Context budget
- Expected excerpts to read: appendToken function, msgHtml function, event handlers

---

## T006: Add CSS styles for thinking blocks

### Objective
Add CSS styling to `web/static/app.css` for thinking blocks so they are visually distinct and properly styled.

### Scope files
- Allowed: `web/static/app.css`
- Avoid touching: `web/static/app.js`

### Testing Strategy
- Test file location: none (CSS testing is visual/manual)
- Test framework: N/A
- Behaviors to assert:
  - CSS classes for thinking blocks (e.g., `.thinking-block`)
  - Toggle button styling for show/hide
  - Visual distinction from regular content blocks

### Read hints
- Grep queries: `.message-content`, `.tool-block`, CSS classes
- Key entrypoints / symbols: Existing CSS patterns for message content, tool blocks

### Details
Add CSS for:
- `.thinking-block` container
- `.thinking-toggle` button (show/hide thinking)
- `.thinking-content` inner container
- Styling to make thinking blocks collapsible and visually distinct

### Acceptance checks
- Behavioral checks: CSS styles applied correctly; toggle button styled
- Commands to run: (visual inspection in browser)

### Context budget
- Expected excerpts to read: Existing CSS patterns, message content styles, tool block styles

---

## Execution Notes

**TDD ordering applied:**
1. T001 (data model) - foundational, no dependencies
2. T002 (config) - independent of T001/003
3. T003 (LLM parsing) - depends on T001 for storage
4. T004 (WebSocket protocol) - depends on T003 agent loop
5. T005 (frontend rendering) - depends on T004 streaming
6. T006 (CSS) - independent but after T005

**Cross-cutting concerns:**
- Ensure thinking tokens are stored alongside messages (T001)
- Ensure thinking config is passed to LLM client (T002 + T003)
- Ensure frontend receives thinking tokens in real-time (T004 + T005)
