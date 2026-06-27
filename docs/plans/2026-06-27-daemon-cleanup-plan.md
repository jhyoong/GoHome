# Daemon Cleanup Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove dead code, reduce handler boilerplate, apply small cleanups, and tidy tests across the daemon-mode branch.

**Architecture:** All changes are purely mechanical refactors -- no behavioral changes. Each task is a self-contained commit that leaves tests green.

**Tech Stack:** Go 1.25, standard library, `go test`, `golangci-lint`

---

### Task 1: Remove dead `session.approval` request path

**Files:**
- Modify: `gohome/internal/tui/client_frontend.go:181-187`
- Modify: `gohome/internal/tui/client_frontend_test.go:131-195`
- Modify: `gohome/internal/daemon/dispatch.go:85-92`
- Modify: `gohome/internal/daemon/rpc/protocol.go:54-58`

**Step 1: Delete `SendApproval()` from `client_frontend.go`**

Remove lines 181-187 (the `SendApproval` method). After removal the file jumps from `SendInput` to `SendCancel`. Also remove the `"guard"` import if it is no longer needed (it is still needed by `RespondApproval` and `ApprovalReqMsg`, so keep it).

**Step 2: Delete `TestClientFrontend_SendApproval` from `client_frontend_test.go`**

Remove the entire test function at lines 131-195.

**Step 3: Delete `SessionApprovalParams` from `protocol.go`**

Remove lines 54-58:
```go
// SessionApprovalParams carries a user's approval decision for a pending tool call.
type SessionApprovalParams struct {
	SessionID string                 `json:"sessionID"`
	Decision  guard.ApprovalDecision `json:"decision"`
}
```

Check whether the `guard` import is still needed in `protocol.go`. It is -- `ApprovalResponseResult` at line 116 still uses `guard.ApprovalDecision`.

**Step 4: Remove the `case MethodSessionApproval:` block in `dispatch.go`**

Remove lines 85-92:
```go
	case rpc.MethodSessionApproval:
		// Approval responses flow through the JSON-RPC response path
		// (msg.IsResponse -> fe.ResolvePending), not as a separate request.
		// This method is unused in the current architecture.
		_ = c.WriteResponse(rpc.Response{
			ID:     msg.ID,
			Result: json.RawMessage(`{}`),
		})
```

**Step 5: Run tests**

Run: `go test ./gohome/...`
Expected: All pass.

**Step 6: Commit**

```bash
git add gohome/internal/tui/client_frontend.go gohome/internal/tui/client_frontend_test.go gohome/internal/daemon/dispatch.go gohome/internal/daemon/rpc/protocol.go
git commit -m "refactor: remove dead session.approval request path"
```

---

### Task 2: Remove `NewStringID()`

**Files:**
- Modify: `gohome/internal/daemon/rpc/message.go:25-28`
- Modify: `gohome/internal/daemon/rpc/message_test.go:154,307`

**Step 1: Delete `NewStringID` from `message.go`**

Remove lines 25-28:
```go
// NewStringID creates a string ID.
func NewStringID(s string) *ID {
	return &ID{str: s, isStr: true}
}
```

**Step 2: Update `TestEncodeErrorResponse` in `message_test.go`**

Line 154 uses `NewStringID("req-1")`. Replace with an inline construction:
```go
id := &ID{str: "req-1", isStr: true}
```

**Step 3: Update `TestIDMarshalUnmarshal` in `message_test.go`**

Line 307 uses `NewStringID("abc")`. Replace with:
```go
{"string", &ID{str: "abc", isStr: true}, `"abc"`},
```

**Step 4: Run tests**

Run: `go test ./gohome/internal/daemon/rpc/...`
Expected: All pass.

**Step 5: Commit**

```bash
git add gohome/internal/daemon/rpc/message.go gohome/internal/daemon/rpc/message_test.go
git commit -m "refactor: remove unused NewStringID constructor"
```

---

### Task 3: Add RPC error code constants

**Files:**
- Modify: `gohome/internal/daemon/rpc/protocol.go`

**Step 1: Add constants to `protocol.go`**

Add after the method constants block (after line 26):

```go
// ---------- Standard JSON-RPC 2.0 error codes ----------

const (
	ErrServerError    = -32000
	ErrMethodNotFound = -32601
	ErrInvalidParams  = -32602
)
```

**Step 2: Run tests**

Run: `go test ./gohome/internal/daemon/rpc/...`
Expected: All pass (additive-only change).

**Step 3: Commit**

```bash
git add gohome/internal/daemon/rpc/protocol.go
git commit -m "refactor: add RPC error code constants"
```

---

### Task 4: Add `respondError`, `respondOK`, `unmarshalParams`, `requireAgent`, and `frontend()` helpers

**Files:**
- Create: `gohome/internal/daemon/helpers.go`
- Modify: `gohome/internal/daemon/dispatch.go`
- Modify: `gohome/internal/daemon/handlers.go`
- Modify: `gohome/internal/daemon/loop.go`

**Step 1: Create `helpers.go` with all helpers**

Create `gohome/internal/daemon/helpers.go`:

```go
package daemon

import (
	"encoding/json"

	"github.com/jhyoong/GoHome/gohome/internal/daemon/frontend"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
)

func respondError(c *rpc.Conn, id *rpc.ID, code int, msg string) {
	_ = c.WriteResponse(rpc.Response{
		ID:    id,
		Error: &rpc.Error{Code: code, Message: msg},
	})
}

func respondOK(c *rpc.Conn, id *rpc.ID, result any) {
	data, _ := json.Marshal(result)
	_ = c.WriteResponse(rpc.Response{ID: id, Result: data})
}

func unmarshalParams(c *rpc.Conn, id *rpc.ID, raw json.RawMessage, v any) bool {
	if err := json.Unmarshal(raw, v); err != nil {
		respondError(c, id, rpc.ErrInvalidParams, "invalid params: "+err.Error())
		return false
	}
	return true
}

func (s *Server) requireAgent(c *rpc.Conn, id *rpc.ID) bool {
	if s.agent == nil {
		respondError(c, id, rpc.ErrServerError, "no agent configured")
		return false
	}
	return true
}

func (s *Server) frontend() *frontend.RPCFrontend {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fe
}
```

**Step 2: Rewrite `dispatch.go` using helpers**

Replace the body of `dispatch()` to use `respondError`, `respondOK`, `unmarshalParams`, and `s.frontend()`. Key changes:

- `s.mu.Lock(); fe := s.fe; s.mu.Unlock()` becomes `fe := s.frontend()`
- All `_ = c.WriteResponse(rpc.Response{ID: msg.ID, Error: ...})` become `respondError(c, msg.ID, code, msg)`
- All `_ = c.WriteResponse(rpc.Response{ID: msg.ID, Result: data})` become `respondOK(c, msg.ID, result)`
- The `json.Unmarshal` + error block in `MethodSessionInput` becomes `if !unmarshalParams(c, msg.ID, msg.Params, &params) { return }`
- Error code literals become `rpc.ErrServerError`, `rpc.ErrMethodNotFound`, `rpc.ErrInvalidParams`

The `MethodDaemonHealth` case uses `respondOK` with the `HealthResult` struct directly. The `MethodDaemonStop` case uses `respondOK` with `json.RawMessage("{}") ` -- but since `respondOK` marshals, pass a `struct{}{}` or use `_ = c.WriteResponse(...)` with raw JSON. Simplest: for empty-body responses, use a small wrapper or keep the raw approach. Use `respondOK(c, msg.ID, struct{}{})` which marshals to `{}`.

**Step 3: Rewrite `handlers.go` using helpers**

Replace all handler bodies:

- `handleSessionList`: replace the error `WriteResponse` with `respondError(c, msg.ID, rpc.ErrServerError, ...)` and the success with `respondOK(c, msg.ID, rpc.SessionListResult{...})`
- `handleSessionNew`: replace the nil check with `if !s.requireAgent(c, msg.ID) { return }`, replace error/success responses with helpers
- `handleSessionResume`: same pattern -- `requireAgent` + `unmarshalParams` + `respondError`/`respondOK`
- `handleSessionCancel`: replace the success response with `respondOK(c, msg.ID, struct{}{})`
- `handleModelSet`: `requireAgent` + `unmarshalParams` + error/success responses via helpers

**Step 4: Rewrite `loop.go` using `s.frontend()`**

In `runLoop()`, replace:
```go
s.mu.Lock()
fe := s.fe
s.mu.Unlock()
```
with:
```go
fe := s.frontend()
```

This appears twice in `runLoop()` (lines 22-24 and accessed again via `s.agent.Frontend = fe`).

**Step 5: Remove now-unused imports**

After rewriting, `dispatch.go` may no longer need `"encoding/json"` directly (since `respondOK` handles marshaling). Check and remove unused imports. `handlers.go` should still need `"encoding/json"` for `json.RawMessage` in `SessionResumeResult.History`.

**Step 6: Run tests**

Run: `go test ./gohome/...`
Expected: All pass.

Run: `golangci-lint run ./gohome/...`
Expected: Clean.

**Step 7: Commit**

```bash
git add gohome/internal/daemon/helpers.go gohome/internal/daemon/dispatch.go gohome/internal/daemon/handlers.go gohome/internal/daemon/loop.go
git commit -m "refactor: add response helpers, reduce handler boilerplate"
```

---

### Task 5: Consolidate `sendRequest` and `sendRequestWithResult` in ClientFrontend

**Files:**
- Modify: `gohome/internal/tui/client_frontend.go:147-289`

**Step 1: Merge into one `sendRequest` that returns `(json.RawMessage, error)`**

Replace the two methods with:

```go
func (cf *ClientFrontend) sendRequest(method string, params any) (json.RawMessage, error) {
	id := cf.idSeq.Add(1)

	paramsData, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	cf.pending.Register(id)

	err = cf.conn.WriteRequest(rpc.Request{
		ID:     rpc.NewID(id),
		Method: method,
		Params: paramsData,
	})
	if err != nil {
		cf.pending.Cancel(id)
		return nil, err
	}

	return cf.pending.Wait(cf.ctx, id)
}
```

**Step 2: Update callers that don't need the result**

`SendInput` and `SendCancel` currently call the old `sendRequest` (which returned `error`). Now they call the new one and discard the first return value:

```go
func (cf *ClientFrontend) SendInput(sessionID, text string) error {
	_, err := cf.sendRequest(rpc.MethodSessionInput, rpc.SessionInputParams{
		SessionID: sessionID,
		Text:      text,
	})
	return err
}
```

Same pattern for `SendCancel`.

**Step 3: Update callers that need the result**

`SendSessionList`, `SendSessionNew`, `SendSessionResume`, `SendModelSet` currently call `sendRequestWithResult`. They now call `sendRequest` directly (it already returns `json.RawMessage`).

**Step 4: Delete `sendRequestWithResult`**

Remove the old method (lines 267-289).

**Step 5: Run tests**

Run: `go test ./gohome/internal/tui/...`
Expected: All pass.

**Step 6: Commit**

```bash
git add gohome/internal/tui/client_frontend.go
git commit -m "refactor: consolidate sendRequest and sendRequestWithResult"
```

---

### Task 6: Small cleanups in server.go and guard/check.go

**Files:**
- Modify: `gohome/internal/daemon/server.go:155-201`
- Modify: `gohome/internal/guard/check.go:69`

**Step 1: Remove duplicate `cancelGraceTimer` in `handleClient`**

In `server.go`, remove line 156:
```go
s.cancelGraceTimer()
```

The call in `Serve()` at line 128 (before `s.handleClient(conn)`) already handles this.

**Step 2: Simplify error return branches in `handleClient`**

Replace lines 194-201:
```go
			if s.ctx.Err() != nil {
				return
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			return
```

With just:
```go
			return
```

Then check if `errors` and `net` imports are still needed in `server.go`. `net` is still used by `net.Listener`, `net.Conn`, `net.ErrClosed` (in `Serve()`), `errors` is still used in `Serve()`. Keep both.

**Step 3: Remove `_ = err` in `guard/check.go`**

Remove line 69 (`_ = err`). The `if err := ...` block and comment already document the intentional ignore. The resulting code:

```go
	case AllowAlways:
		if err := g.whitelist.AddProject(tool, dec.SavedPattern); err != nil {
			// Log but don't fail the allow -- the user said yes.
			// Future calls will re-prompt, which is acceptable.
		}
		return Decision{Allow: true, Reason: "user_always", SavedPattern: dec.SavedPattern}, nil
```

Note: `golangci-lint` may flag the empty `if` body. If so, replace with `slog.Warn("whitelist persist failed", "err", err)` which is more useful anyway. This requires adding `"log/slog"` to imports.

**Step 4: Run tests**

Run: `go test ./gohome/...`
Expected: All pass.

Run: `golangci-lint run ./gohome/...`
Expected: Clean.

**Step 5: Commit**

```bash
git add gohome/internal/daemon/server.go gohome/internal/guard/check.go
git commit -m "refactor: remove duplicate cancelGraceTimer, simplify error branches, clean up _ = err"
```

---

### Task 7: Tidy `client_frontend_test.go`

**Files:**
- Modify: `gohome/internal/tui/client_frontend_test.go`

**Step 1: Move `readResult` to package level**

Add at the top of the file (after imports):

```go
type readResult struct {
	msg *rpc.Message
	err error
}
```

Then delete the 4 identical function-scoped definitions at the old lines (in `TestClientFrontend_SendInput`, `TestClientFrontend_SendCancel`, `TestClientFrontend_RespondApproval`, and -- if it still exists -- `TestClientFrontend_SendApproval`, which was deleted in Task 1).

**Step 2: Extract `setupClientFrontend` helper**

Add a helper near the top:

```go
func setupClientFrontend(t *testing.T) (cf *ClientFrontend, daemonConn *rpc.Conn, events chan AgentEventMsg, cleanup func()) {
	t.Helper()
	daemonRaw, tuiRaw := net.Pipe()
	daemonConn = rpc.NewConn(daemonRaw)
	tuiConn := rpc.NewConn(tuiRaw)
	events = make(chan AgentEventMsg, 4)
	cf = NewClientFrontend(tuiConn, events)
	cleanup = func() {
		daemonRaw.Close()
		tuiRaw.Close()
	}
	return
}
```

**Step 3: Rewrite each test to use the helpers**

For example, `TestClientFrontend_SendInput` becomes:

```go
func TestClientFrontend_SendInput(t *testing.T) {
	cf, daemonConn, _, cleanup := setupClientFrontend(t)
	defer cleanup()

	go cf.ReadLoop()

	ch := make(chan readResult, 1)
	go func() {
		msg, err := daemonConn.Read()
		ch <- readResult{msg, err}
		if msg != nil && msg.ID != nil {
			_ = daemonConn.WriteResponse(rpc.Response{
				ID:     msg.ID,
				Result: json.RawMessage(`{}`),
			})
		}
	}()

	// ... rest unchanged
}
```

Apply the same pattern to all remaining tests: `TestClientFrontend_ReceivesAgentEvent`, `TestClientFrontend_SendCancel`, `TestClientFrontend_RespondApproval`, `TestClientFrontend_ReceivesApprovalRequest`, `TestClientFrontend_ReceivesSessionState`, `TestClientFrontend_Close`.

**Step 4: Run tests**

Run: `go test ./gohome/internal/tui/ -run TestClientFrontend -v`
Expected: All pass.

**Step 5: Commit**

```bash
git add gohome/internal/tui/client_frontend_test.go
git commit -m "refactor: extract shared helpers in client_frontend_test.go"
```

---

### Task 8: Tidy `server_test.go`

**Files:**
- Modify: `gohome/internal/daemon/server_test.go`

**Step 1: Add test helpers at the top of the file (after imports, before first test)**

```go
func newTestSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "gh-daemon-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "t.sock")
}

func serveBackground(t *testing.T, srv *Server) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		srv.Serve()
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	t.Cleanup(func() {
		srv.Stop()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("server did not stop within 2s")
		}
	})
}

func newTestGuard() *guard.Guard {
	wl := &guard.Whitelist{}
	g := guard.NewGuard(wl, &noopApprover{})
	g.SetYolo(true)
	return g
}

func dialTestServer(t *testing.T, sockPath string) net.Conn {
	t.Helper()
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}
```

**Step 2: Rewrite tests to use helpers**

For each test that creates its own socket dir, guard, serve goroutine, and dial:

- Replace `os.MkdirTemp` + `defer os.RemoveAll` + `filepath.Join` with `sock := newTestSocket(t)`
- Replace `wl := &guard.Whitelist{}; g := guard.NewGuard(wl, &noopApprover{}); g.SetYolo(true)` with `g := newTestGuard()`
- Replace the `wg.Add(1); go func() { defer wg.Done(); srv.Serve() }(); time.Sleep(...)` with `serveBackground(t, srv)` and remove the `srv.Stop(); wg.Wait()` at the end of each test
- Replace `net.Dial("unix", sock)` + error check + `defer conn.Close()` with `conn := dialTestServer(t, sock)`

Apply to: `TestServer_HealthCheck`, `TestServer_Stop`, `TestServer_WithAgent_ProcessesInput`, `TestServer_SessionCancel`, `TestServer_SessionList`, `TestServer_Reconnect_SendsState`, `TestServer_GracePeriod_ExitsWhenIdle`, `TestServer_GracePeriod_CancelledByReconnect`.

Note: `TestServer_HealthCheck` and `TestServer_Stop` use `t.TempDir()` for the socket path, which works because their paths are short enough. Keep these using `t.TempDir()` if preferred, or unify with `newTestSocket`. The `TestServer_GracePeriod_*` tests and `TestServer_Stop` need the `done` channel for explicit shutdown assertions -- for those, use the goroutine directly instead of `serveBackground` since they test the shutdown behavior itself.

**Step 3: Run tests**

Run: `go test ./gohome/internal/daemon/ -v`
Expected: All pass.

**Step 4: Commit**

```bash
git add gohome/internal/daemon/server_test.go
git commit -m "refactor: extract shared helpers in server_test.go"
```

---

### Task 9: Tidy `message_test.go`

**Files:**
- Modify: `gohome/internal/daemon/rpc/message_test.go`

**Step 1: Add `assertJSONField` helper**

Add near the top of the file:

```go
func assertJSONField(t *testing.T, raw map[string]json.RawMessage, field, want string) {
	t.Helper()
	got, ok := raw[field]
	if !ok {
		t.Fatalf("missing %q field", field)
	}
	if string(got) != want {
		t.Fatalf("%s = %s, want %s", field, got, want)
	}
}

func assertNoJSONField(t *testing.T, raw map[string]json.RawMessage, field string) {
	t.Helper()
	if _, ok := raw[field]; ok {
		t.Fatalf("unexpected %q field present", field)
	}
}

func marshalToRaw(t *testing.T, v any) map[string]json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	return raw
}
```

**Step 2: Rewrite encode tests using helpers**

`TestEncodeRequest` becomes:

```go
func TestEncodeRequest(t *testing.T) {
	req := Request{
		ID:     NewID(42),
		Method: "tools/list",
		Params: json.RawMessage(`{"cursor":"abc"}`),
	}
	raw := marshalToRaw(t, req)

	assertJSONField(t, raw, "jsonrpc", `"2.0"`)
	assertJSONField(t, raw, "id", "42")
	assertJSONField(t, raw, "method", `"tools/list"`)
	assertJSONField(t, raw, "params", `{"cursor":"abc"}`)
}
```

Apply similar rewrites to `TestEncodeNotification`, `TestEncodeResponse`, `TestEncodeErrorResponse`. Use `assertNoJSONField` for the "must NOT include" checks.

**Step 3: Run tests**

Run: `go test ./gohome/internal/daemon/rpc/ -v`
Expected: All pass.

**Step 4: Commit**

```bash
git add gohome/internal/daemon/rpc/message_test.go
git commit -m "refactor: extract JSON assertion helpers in message_test.go"
```

---

### Task 10: Tidy `frontend_test.go` and `conn_test.go`

**Files:**
- Modify: `gohome/internal/daemon/frontend/frontend_test.go`
- Modify: `gohome/internal/daemon/rpc/conn_test.go`

**Step 1: Add `setupTestFrontend` helper in `frontend_test.go`**

```go
func setupTestFrontend(t *testing.T) (fe *frontend.RPCFrontend, tuiRPC *rpc.Conn, daemonRPC *rpc.Conn, cleanup func()) {
	t.Helper()
	daemonConn, tuiConn := net.Pipe()
	daemonRPC = rpc.NewConn(daemonConn)
	tuiRPC = rpc.NewConn(tuiConn)
	fe = frontend.New(daemonRPC)
	cleanup = func() {
		daemonConn.Close()
		tuiConn.Close()
	}
	return
}
```

Rewrite the 5 tests to use it. For example, `TestRPCFrontend_Emit`:

```go
func TestRPCFrontend_Emit(t *testing.T) {
	fe, tuiRPC, _, cleanup := setupTestFrontend(t)
	defer cleanup()

	// ... rest unchanged, but using tuiRPC and fe from the helper
}
```

Note: `TestRPCFrontend_AwaitUserInput` and `TestRPCFrontend_AwaitUserInput_ContextCancelled` don't use `tuiRPC` -- they can assign `_ = tuiRPC` or just ignore it.

**Step 2: Add `setupConnPair` helper in `conn_test.go`**

```go
func setupConnPair(t *testing.T) (conn1, conn2 *Conn, cleanup func()) {
	t.Helper()
	c1, c2 := net.Pipe()
	cleanup = func() {
		c1.Close()
		c2.Close()
	}
	return NewConn(c1), NewConn(c2), cleanup
}
```

Rewrite the 3 tests to use it.

**Step 3: Run tests**

Run: `go test ./gohome/internal/daemon/...`
Expected: All pass.

**Step 4: Commit**

```bash
git add gohome/internal/daemon/frontend/frontend_test.go gohome/internal/daemon/rpc/conn_test.go
git commit -m "refactor: extract pipe setup helpers in frontend_test.go and conn_test.go"
```

---

### Task 11: Final verification

**Step 1: Run full test suite**

Run: `go test ./gohome/...`
Expected: All pass.

**Step 2: Run linter**

Run: `golangci-lint run ./gohome/...`
Expected: Clean.

**Step 3: Run vet**

Run: `go vet ./gohome/...`
Expected: Clean.
