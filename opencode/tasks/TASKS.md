# Tasks: Fix Thinking Tokens Issues

| ID   | Title | Status |
|------|-------|--------|
| T001 | Add ThinkingTokens to LLM request body | [x] |
| T002 | Persist thinking content to SQLite via agent loop | [ ] |

---

## T001: Add ThinkingTokens to LLM request body

### Objective
Add the `ThinkingTokens` field to the LLM request body struct and set it when calling the API during streaming.

### Scope files
- Allowed: `internal/llm/client.go`
- Avoid touching: `internal/agent/loop.go`, `internal/session/store.go`

### Testing Strategy
- Test file location: `internal/llm/client_test.go`
- Test framework: `go test`
- Behaviors to assert (be specific):
  - `reqBody` struct given no fields set should have `ThinkingTokens` field serialized to JSON as `"thinking_tokens"`
  - `Stream()` method given a client with `ThinkingTokens` configured should send `thinking_tokens` in the JSON body to the endpoint
  - `Stream()` method given zero `ThinkingTokens` should send `"thinking_tokens": 0` in the JSON body
- Mock/stub requirements: Use `httptest.NewServer` to capture and inspect the request body JSON
- Must NOT test: Tool call handling, prediction callbacks, usage reporting

### Read hints
- Grep queries: `ThinkingTokens`, `reqBody`, `Stream`
- Key entrypoints / symbols: `llm.Client.Stream()`, `llm.reqBody`, `config.EndpointConfig`

### Details
1. Add `ThinkingTokens int` field to the `reqBody` struct (around line 87) with JSON tag `json:"thinking_tokens,omitempty"`
2. In the `Stream()` method, set `ThinkingTokens: c.cfg.ThinkingTokens` when building the `reqBody` (around line 151)
3. Ensure the field uses `omitempty` JSON tag so it only appears when non-zero (or always if that matches API requirements)

### Acceptance checks
- Behavioral checks: JSON body sent to API contains `thinking_tokens` field from config
- Commands to run: `go test ./internal/llm -v`

### Context budget
- Expected excerpts to read: `client.go` lines 79-87 and 147-151 only
- Notes to keep context small: Only modify the request struct and Stream method body; do not change callback signatures

---

## T002: Persist thinking content to SQLite via agent loop

### Objective
Accumulate thinking content during agent loop streaming and save it alongside assistant messages in SQLite.

### Scope files
- Allowed: `internal/agent/loop.go`
- Avoid touching: `internal/llm/client.go`, `internal/session/store.go`

### Testing Strategy
- Test file location: `internal/session/store_test.go` (existing tests cover persistence; this task verifies integration)
- Test framework: `go test`
- Behaviors to assert (be specific):
  - `agent.Loop.Run()` given a callback that accumulates thinking should call `onThinking` with prediction content during streaming
  - `agent.Loop.Run()` after streaming completes should save assistant message with `Thinking` field populated
  - `agent.Loop.Run()` when no thinking content accumulated should save assistant message with empty `Thinking` field
- Mock/stub requirements: Mock `llm.Client.Stream()` to return streaming deltas; mock `session.Store` to capture saved messages
- Must NOT test: Tool execution, approval broker, steering channel handling

### Read hints
- Grep queries: `onThinking`, `finalText`, `AddMessage`
- Key entrypoints / symbols: `agent.Loop.Run()`, `session.Message`, `session.Store.AddMessage()`

### Details
1. Add a `thinkingText` string variable to accumulate thinking content (around line 82, near `finalText`)
2. Create a wrapper callback that accumulates prediction content into `thinkingText` and calls the original `onThinking`
3. At line 163-168, include `Thinking: thinkingText` when calling `l.store.AddMessage()` for the assistant response
4. Ensure empty thinking content is saved as empty string (not nil) to match schema expectations

### Acceptance checks
- Behavioral checks: Messages saved with non-empty Thinking are persisted correctly; empty Thinking is saved as empty string
- Commands to run: `go test ./internal/session -v`

### Context budget
- Expected excerpts to read: `loop.go` lines 80-95 and 163-168 only
- Notes to keep context small: Only add accumulation logic near existing `finalText` pattern; do not change API signatures