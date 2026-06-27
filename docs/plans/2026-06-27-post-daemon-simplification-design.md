# Post-Daemon Simplification Design

Date: 2026-06-27

## Context

The daemon mode introduction (commits 055de4a through f80d15e) changed the
architecture from "TUI runs agent in-process" to "daemon owns the agent, TUI
is a JSON-RPC client." The old in-process path is now dead code, and the daemon
server has grown to 700 lines in a single file. This design covers two phases
of cleanup.

## Phase 1: Dead Code Removal

### 1.1 Remove `tui.Frontend`

The `tui.Frontend` struct in `frontend.go` implemented `agent.Frontend` for the
old in-process path. It is no longer instantiated in production code (only in 4
test files). Delete:

- `Frontend` struct and all methods (`Emit`, `RequestApproval`, `AwaitUserInput`)
- `NewFrontend()` constructor
- Compile-time assertions (`_ agent.Frontend = (*Frontend)(nil)`, `_ guard.Frontend = (*Frontend)(nil)`)
- `inputCh chan string` field from `Model`

Change `tui.New()` signature from `New(fe *Frontend, sessionID string)` to
`New(sessionID string)`. The `Model` always receives a `ClientFrontend` via
`SetClientFrontend()`.

`frontend.go` retains only the message type definitions (`AgentEventMsg`,
`ExternalEditorMsg` and their aliases).

### 1.2 Unify Approval Message Types

Collapse `ApprovalReqMsg` (channel-based) and `ClientApprovalReqMsg`
(RPC-based) into a single `ApprovalReqMsg` that carries an `*rpc.ID`. Delete
the old channel-based type.

- Merge `handleApprovalReq()` and `handleClientApprovalReq()` into one handler
- `resolveApproval()` always calls `m.cfe.RespondApproval()`
- `approvalPrompt` drops the `reply chan` field; uses `rpcID` only
- `Model.Update()` has a single `case ApprovalReqMsg:` branch

### 1.3 Unify Input Path

`sendInputCmd()` currently branches on `cfe != nil`. Remove the branch -- it
always uses `cfe.SendInput()`. Remove the `inputCh` field from `Model`.

### 1.4 Deduplicate `newSessionID()`

Move to `session.NewID()` in the `session` package. Delete the duplicate
implementations in `main.go` and `daemon/server.go`.

### 1.5 Remove `pickResume()`

Delete the function from `main.go`. It is dead code. Keep the `--resume` flag
and its warning message for future implementation.

### 1.6 Test Updates

Tests that used `NewFrontend()` (4 call sites in `tui_test.go` and
`integration_test.go`) are rewritten to push `AgentEventMsg` directly into
`Model.Update()`, consistent with the snapshot test approach. For tests that
need to simulate user input submission, add a test-only helper.

## Phase 2: Server Extraction

### 2.1 Split `daemon/server.go`

The 697-line file is split into four files, all in `package daemon`:

| File | Contents |
|---|---|
| `server.go` | `Server` struct, `NewServer`, `Serve`, `Stop`, `handleClient`, `cleanup`, grace period |
| `dispatch.go` | `dispatch()`, `initAgent` |
| `handlers.go` | `handleSessionList`, `handleSessionNew`, `handleSessionResume`, `handleSessionCancel`, `handleModelSet` |
| `loop.go` | `runLoop`, `sendStateSync` |

No interface changes, no import changes, no behavioral changes. Purely a file
reorganization.

### 2.2 Keep `daemon/frontend` Separate

The `daemon/frontend` subpackage (133 lines, one type) stays as-is. Merging it
into `daemon` risks circular dependencies without meaningful simplification.

## What Stays Unchanged

- `guard.Frontend` interface (used by guard engine, implemented by `RPCFrontend`)
- `agent.Frontend` interface (unchanged)
- `tui.Model` structure (no decomposition in this round)
- All daemon RPC protocol types in `daemon/rpc`
- `ClientFrontend` in `tui/client_frontend.go`

## Testing Strategy

- All existing snapshot tests continue to work (they use `Model.Update()` directly)
- The 4 tests using `NewFrontend()` are rewritten
- `go test ./gohome/...` must pass after each phase
- `golangci-lint run ./gohome/...` must pass
