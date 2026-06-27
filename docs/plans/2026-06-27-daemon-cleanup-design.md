# Daemon Cleanup Design

Date: 2026-06-27

## Context

The daemon-mode branch introduced ~7000 lines of new code across
`internal/daemon/`, `internal/tui/client_frontend.go`, and a rewritten
`main.go`. A first round of simplification (post-daemon Phase 1 and Phase 2)
removed the old `tui.Frontend`, unified approval types, deduplicated
`newSessionID`, and split `server.go` into four files.

This design covers a second round: dead code that survived, handler boilerplate
reduction, small cleanups, and test tidying.

## Phase 1: Dead Code Removal

### 1.1 Remove the dead `session.approval` request path

The approval flow uses `RespondApproval()` (a JSON-RPC response from TUI to
daemon), not `SendApproval()` (a JSON-RPC request). The request-based path is
unused and the dispatch handler even says so in a comment.

Delete:

| What | Where |
|---|---|
| `SendApproval()` method | `tui/client_frontend.go:182-187` |
| `SessionApprovalParams` type | `daemon/rpc/protocol.go:54-58` |
| `case MethodSessionApproval:` block | `daemon/dispatch.go:85-92` |
| `TestClientFrontend_SendApproval` test | `tui/client_frontend_test.go` |

Keep `MethodSessionApproval` constant (documents the protocol, costs nothing).

### 1.2 Remove `NewStringID()`

`rpc/message.go:26` -- only used in two test rows. The daemon exclusively uses
numeric IDs via `NewID()`. Delete the constructor and update or remove the two
test rows that use it.

## Phase 2: Handler Boilerplate Reduction

### 2.1 Add `respondError` and `respondOK` helpers

Two package-private free functions in `package daemon`:

```go
func respondError(c *rpc.Conn, id *rpc.ID, code int, msg string) {
    _ = c.WriteResponse(rpc.Response{
        ID: id, Error: &rpc.Error{Code: code, Message: msg},
    })
}

func respondOK(c *rpc.Conn, id *rpc.ID, result any) {
    data, _ := json.Marshal(result)
    _ = c.WriteResponse(rpc.Response{ID: id, Result: data})
}
```

Replaces 25+ verbose `WriteResponse` call sites across `dispatch.go` and
`handlers.go`.

### 2.2 Add `requireAgent` helper

```go
func (s *Server) requireAgent(c *rpc.Conn, id *rpc.ID) bool {
    if s.agent == nil {
        respondError(c, id, -32000, "no agent configured")
        return false
    }
    return true
}
```

Replaces 3 identical nil-check blocks in `handleSessionNew`,
`handleSessionResume`, and `handleModelSet`.

### 2.3 Add `unmarshalParams` helper

```go
func unmarshalParams(c *rpc.Conn, id *rpc.ID, raw json.RawMessage, v any) bool {
    if err := json.Unmarshal(raw, v); err != nil {
        respondError(c, id, -32602, "invalid params: "+err.Error())
        return false
    }
    return true
}
```

Replaces 3 identical unmarshal-and-error blocks.

### 2.4 Add RPC error code constants

In `rpc/protocol.go`:

```go
const (
    ErrCodeServerError    = -32000
    ErrCodeMethodNotFound = -32601
    ErrCodeInvalidParams  = -32602
)
```

Used by the helpers and the default case in `dispatch()`.

### 2.5 Consolidate `sendRequest` and `sendRequestWithResult`

In `tui/client_frontend.go`, merge into a single method that returns
`(json.RawMessage, error)`. Callers that don't need the result ignore it.

### 2.6 Add `frontend()` accessor

```go
func (s *Server) frontend() *frontend.RPCFrontend {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.fe
}
```

Replaces 3+ inline lock-fetch-unlock patterns in `dispatch.go` and `loop.go`.

## Phase 3: Small Cleanups

### 3.1 Remove duplicate `cancelGraceTimer` call

`Serve()` calls `cancelGraceTimer()` before entering `handleClient()`.
`handleClient()` calls it again as its first statement. Remove the call inside
`handleClient`.

### 3.2 Simplify `handleClient` error return branches

The read-loop error handling has three branches that all return:

```go
if s.ctx.Err() != nil { return }
if errors.Is(err, net.ErrClosed) { return }
return
```

Simplify to just `return`.

### 3.3 Clean up `_ = err` in `guard/check.go`

Line 69 has `_ = err` after `AddProject` fails. Remove the `_ = err` line --
the surrounding `if err :=` and comment already make the intentional ignore
clear.

## Phase 4: Test Tidying

### 4.1 `client_frontend_test.go`

- Move `readResult` struct to package-level (defined identically 4 times)
- Extract `setupClientFrontend(t)` helper to eliminate 9 pipe+conn+events
  setup repetitions
- Extract `readFromDaemon(conn)` helper for the recurring goroutine pattern

### 4.2 `server_test.go`

- Extract `newTestSocket(t)` -- temp dir, cleanup, returns socket path
- Extract `serveBackground(t, srv)` -- goroutine + cleanup
- Extract `newTestGuard(yolo bool)` -- whitelist + guard init
- Extract `dialTestServer(t, sockPath)` -- net.Dial + cleanup

### 4.3 `message_test.go`

- Extract `assertJSONField(t, raw, field, expected)` to replace the repeated
  field-check-and-compare pattern used 15+ times in encode tests

### 4.4 `frontend_test.go` and `conn_test.go`

- `frontend_test.go`: Extract `setupTestFrontend(t)` (5 pipe setups)
- `conn_test.go`: Extract `setupConnPair(t)` (3 pipe setups)

### 4.5 `main_test.go` -- no changes

Only 12 lines. Not worth refactoring.

## What Stays Unchanged

- All daemon RPC protocol types (except `SessionApprovalParams`)
- `RPCFrontend` in `daemon/frontend/` (already clean)
- `guard.Frontend` interface
- `agent.Frontend` interface
- `rpc.Pending` (shared by both sides, well-factored)
- `rpc.Conn` read/write logic

## Testing Strategy

- All existing tests continue to pass after each phase
- Test helpers are purely mechanical extractions -- no behavioral changes
- `go test ./gohome/...` and `golangci-lint run ./gohome/...` must pass
  after each phase
