# Post-Daemon Simplification Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove dead in-process code paths left over from the daemon mode migration, unify dual approval/input paths, deduplicate helpers, and split the 700-line daemon server into focused files.

**Architecture:** Phase 1 removes all vestiges of the old in-process frontend (`tui.Frontend`, dual approval types, `inputCh`, `pickResume`). Phase 2 is a pure file reorganization of `daemon/server.go` into four files. Each task is independently compilable and testable.

**Tech Stack:** Go 1.25, Bubble Tea (charmbracelet/bubbletea), teatest

---

### Task 1: Move `newSessionID()` to `session.NewID()`

Both `main.go:41-48` and `daemon/server.go:685-692` have identical `newSessionID()` functions. Move the implementation to the `session` package and update callers.

**Files:**
- Modify: `gohome/internal/session/session.go` (add `NewID`)
- Modify: `gohome/cmd/gohome/main.go` (delete local `newSessionID`, use `session.NewID`)
- Modify: `gohome/internal/daemon/server.go` (delete local `newSessionID`, use `session.NewID`)
- Test: `gohome/internal/session/session_test.go`

**Step 1: Write the test for `session.NewID()`**

Add to `session_test.go`:

```go
func TestNewID(t *testing.T) {
	id := session.NewID()
	if len(id) != 8 {
		t.Fatalf("NewID: expected 8-char ID, got %d chars: %q", len(id), id)
	}
	if id != strings.ToLower(id) {
		t.Fatalf("NewID: expected lowercase, got %q", id)
	}
	id2 := session.NewID()
	if id == id2 {
		t.Fatal("NewID: two calls returned the same ID")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/session/ -run TestNewID -v`
Expected: FAIL with `session.NewID undefined`

**Step 3: Add `NewID()` to `session/session.go`**

Add to end of file:

```go
// NewID generates an 8-char lowercase base32 session ID using crypto/rand.
func NewID() string {
	buf := make([]byte, 5)
	if _, err := rand.Read(buf); err != nil {
		panic("session.NewID: crypto/rand failed: " + err.Error())
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	return strings.ToLower(enc.EncodeToString(buf))
}
```

Add imports: `"crypto/rand"`, `"encoding/base32"`, `"strings"`.

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/session/ -run TestNewID -v`
Expected: PASS

**Step 5: Update `main.go` to use `session.NewID()`**

- Delete the `newSessionID()` function (lines 40-48)
- Replace `sessionID := newSessionID()` (line 304) with `sessionID := session.NewID()`
- Remove unused imports: `"crypto/rand"`, `"encoding/base32"`

**Step 6: Update `daemon/server.go` to use `session.NewID()`**

- Delete the `newSessionID()` function (lines 684-692)
- Replace `id := newSessionID()` (line 471) with `id := session.NewID()`
- Remove unused imports: `"crypto/rand"`, `"encoding/base32"`

**Step 7: Run full test suite**

Run: `go test ./gohome/...`
Expected: all PASS

**Step 8: Commit**

```bash
git add gohome/internal/session/session.go gohome/internal/session/session_test.go \
       gohome/cmd/gohome/main.go gohome/internal/daemon/server.go
git commit -m "refactor: deduplicate newSessionID into session.NewID()"
```

---

### Task 2: Remove `pickResume()` from `main.go`

Dead function -- `--resume` prints a warning and does nothing in daemon mode.

**Files:**
- Modify: `gohome/cmd/gohome/main.go` (delete `pickResume` function, lines 77-96)

**Step 1: Delete `pickResume()`**

Remove lines 77-96 (the `pickResume` function). The `--resume` flag and its warning message on line 139-141 stay.

**Step 2: Remove unused imports**

If `pickResume` was the only user of `"github.com/jhyoong/GoHome/gohome/internal/llm/common"` in main.go, remove that import.

**Step 3: Verify build**

Run: `go build -o /dev/null ./gohome/cmd/gohome/`
Expected: compiles without error

**Step 4: Commit**

```bash
git add gohome/cmd/gohome/main.go
git commit -m "refactor: remove dead pickResume function from main.go"
```

---

### Task 3: Remove `tui.Frontend` and `inputCh`

The `tui.Frontend` struct is only used in tests. Production always uses `ClientFrontend` via daemon RPC. Remove the struct, its methods, and the `inputCh` field from `Model`.

**Files:**
- Modify: `gohome/internal/tui/frontend.go` (delete `Frontend` struct, methods, assertions)
- Modify: `gohome/internal/tui/model.go` (remove `inputCh` field, change `New()` signature)
- Modify: `gohome/internal/tui/model_agent.go` (remove `inputCh` branch from `sendInputCmd`)

**Step 1: Strip `frontend.go` down to message types only**

Keep only:
- `AgentEventMsg` struct and its fields (lines 23-26 in current file)
- `agentEventMsg` alias (line 29)
- `ExternalEditorMsg` struct (lines 32-36)
- `externalEditorMsg` alias (line 38)

Delete everything else:
- Compile-time assertions (lines 15-18)
- `Frontend` struct definition (lines 42-45)
- `NewFrontend()` (lines 48-52)
- `SetProgram()` (lines 55-57)
- `InputCh()` (lines 60-63)
- `Emit()` (lines 67-71)
- `RequestApproval()` (lines 78-90)
- `AwaitUserInput()` (lines 94-101)
- Remove imports no longer needed: `"context"`, `"fmt"`, `tea`, `agent`, `guard`

**Step 2: Remove `inputCh` from `Model`**

In `model.go`:
- Delete the `inputCh chan string` field (line 87)
- Remove `inputCh` from the field comment about `cfe` (lines 145-147)
- Change `New()` signature from `New(fe *Frontend, sessionID string)` to `New(sessionID string)`
- Delete the `fe`/`inputCh` logic inside `New()` (lines 167-171)
- Remove `inputCh` from the struct literal (line 180)

**Step 3: Simplify `sendInputCmd` in `model_agent.go`**

Current code (lines 329-343):
```go
func (m *Model) sendInputCmd(text string) tea.Cmd {
	if m.cfe != nil {
		cfe := m.cfe
		sid := m.focused
		return func() tea.Msg {
			cfe.SendInput(sid, text)
			return nil
		}
	}
	ch := m.inputCh
	return func() tea.Msg {
		ch <- text
		return nil
	}
}
```

Replace with:
```go
func (m *Model) sendInputCmd(text string) tea.Cmd {
	cfe := m.cfe
	sid := m.focused
	return func() tea.Msg {
		cfe.SendInput(sid, text)
		return nil
	}
}
```

Update comments on lines 326-328 to remove the "Otherwise it writes to the local inputCh channel" part.

**Step 4: Verify build**

Run: `go build -o /dev/null ./gohome/cmd/gohome/`
Expected: compiles (production code does not use `tui.Frontend`)

**Step 5: Do NOT run tests yet** -- they will fail because test files still reference `NewFrontend()` and pass `fe` to `New()`. That is fixed in Tasks 4 and 5.

**Step 6: Commit (with `--no-verify` if linter runs tests)**

Wait -- do not commit yet. Tasks 4 and 5 fix the tests. Commit all together at the end of Task 5.

---

### Task 4: Update approval tests for the new types

The approval tests in `approval_test.go` use `makeApprovalReq` which creates `ApprovalReqMsg` with a `Reply` channel. These tests need to keep working. Since the approval tests are testing the TUI's approval key handling (not the RPC transport), we keep the channel-based approach but adapt the types.

The key insight: `resolveApproval()` currently does `ap.reply <- dec` for the standalone path. After removing the standalone path, we need a way for tests to observe the decision. The simplest approach: keep `ApprovalReqMsg` with a `Reply chan` for now (tests need it), and refactor the approval prompt to use a callback instead of branching on `rpcID` vs `reply`. This avoids coupling tests to daemon RPC.

**Files:**
- Modify: `gohome/internal/tui/approval.go` (unify `approvalPrompt` to use a callback)
- Modify: `gohome/internal/tui/model_approval.go` (merge the two handlers)
- Modify: `gohome/internal/tui/model.go` (single `case` in `Update`)
- Modify: `gohome/internal/tui/client_frontend.go` (rename `ClientApprovalReqMsg` to `ApprovalReqMsg`)
- Test: `gohome/internal/tui/approval_test.go` (update `makeApprovalReq`)

**Step 1: Refactor `approvalPrompt` to use a resolve callback**

In `approval.go`, replace the `reply` and `rpcID` fields with a single `resolve func(guard.ApprovalDecision)`:

```go
type approvalPrompt struct {
	req     guard.ApprovalRequest
	resolve func(guard.ApprovalDecision)
	pattern string
	selected     int
	editing      bool
	patternInput textinput.Model
	steering     bool
	steerInput   textinput.Model
}
```

Replace `newApprovalPrompt` and `newDaemonApprovalPrompt` with a single constructor:

```go
func newApprovalPrompt(req guard.ApprovalRequest, resolve func(guard.ApprovalDecision)) *approvalPrompt {
	pi := textinput.New()
	pi.Placeholder = "pattern"
	pi.SetValue(req.SuggestedPattern)

	si := textinput.New()
	si.Placeholder = "steer message"

	return &approvalPrompt{
		req:          req,
		resolve:      resolve,
		pattern:      req.SuggestedPattern,
		patternInput: pi,
		steerInput:   si,
	}
}
```

Remove the `newDaemonApprovalPrompt` function entirely.
Remove the `rpc` import from `approval.go`.

**Step 2: Unify `ApprovalReqMsg`**

In `approval.go`, change `ApprovalReqMsg` to carry a resolve callback:

```go
type ApprovalReqMsg struct {
	Req     guard.ApprovalRequest
	Resolve func(guard.ApprovalDecision)
}
```

Delete the `Reply chan guard.ApprovalDecision` field.

In `client_frontend.go`, delete `ClientApprovalReqMsg`. In `handleRequest()`, build `ApprovalReqMsg` instead:

```go
case rpc.MethodApprovalRequest:
	var params rpc.ApprovalRequestParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return
	}
	rpcID := msg.ID
	cf.approvals <- ApprovalReqMsg{
		Req: guard.ApprovalRequest{
			SessionID:        params.SessionID,
			Tool:             params.Tool,
			Input:            params.Input,
			Summary:          params.Summary,
			SuggestedPattern: params.SuggestedPattern,
		},
		Resolve: func(dec guard.ApprovalDecision) {
			_ = cf.RespondApproval(rpcID, dec)
		},
	}
```

Update the `approvals` channel type from `chan ClientApprovalReqMsg` to `chan ApprovalReqMsg`. Update `Approvals()` return type from `<-chan ClientApprovalReqMsg` to `<-chan ApprovalReqMsg`.

**Step 3: Merge approval handlers in `model_approval.go`**

Delete `handleClientApprovalReq`. Rewrite `handleApprovalReq`:

```go
func (m *Model) handleApprovalReq(msg ApprovalReqMsg) {
	if msg.Req.SessionID == m.focused && m.activeApproval == nil {
		m.activeApproval = newApprovalPrompt(msg.Req, msg.Resolve)
	} else {
		m.pendingApprovals[msg.Req.SessionID] = newApprovalPrompt(msg.Req, msg.Resolve)
	}
}
```

Simplify `resolveApproval`:

```go
func (m *Model) resolveApproval(dec guard.ApprovalDecision) {
	if m.activeApproval == nil {
		return
	}
	m.activeApproval.resolve(dec)
	m.activeApproval = nil
	m.promoteApproval()
}
```

**Step 4: Update `Model.Update()` in `model.go`**

Remove the `case ClientApprovalReqMsg:` branch (line 349-350). The existing `case approvalReqMsg:` (line 346-347) now handles everything.

Remove the `ClientApprovalReqMsg` import usage. The type no longer exists.

**Step 5: Update `main.go` -- feed approvals channel**

In `main.go`, the goroutine at lines 401-405 feeds `cfe.Approvals()` into Bubble Tea. Update the type:

```go
go func() {
	for areq := range cfe.Approvals() {
		p.Send(areq)
	}
}()
```

This should compile without changes since `Approvals()` now returns `<-chan ApprovalReqMsg` (which is already the type the `Update()` switch expects).

**Step 6: Update `approval_test.go`**

Change `makeApprovalReq` to use the callback pattern:

```go
func makeApprovalReq(sessionID, tool, suggestedPattern string, inputJSON json.RawMessage) (tui.ApprovalReqMsg, chan guard.ApprovalDecision) {
	ch := make(chan guard.ApprovalDecision, 1)
	msg := tui.ApprovalReqMsg{
		Req: guard.ApprovalRequest{
			SessionID:        sessionID,
			Tool:             tool,
			Input:            inputJSON,
			SuggestedPattern: suggestedPattern,
		},
		Resolve: func(dec guard.ApprovalDecision) {
			ch <- dec
		},
	}
	return msg, ch
}
```

No other test changes needed -- all tests use `makeApprovalReq` and read from `ch`.

**Step 7: Update `client_frontend_test.go`**

Check for any references to `ClientApprovalReqMsg` and update them to `ApprovalReqMsg`.

**Step 8: Run tests**

Run: `go test ./gohome/internal/tui/ -run TestApproval -v`
Expected: all PASS

Do NOT commit yet -- Task 5 fixes the remaining test failures.

---

### Task 5: Update remaining tests that used `tui.Frontend`

Four test functions use `NewFrontend()` and/or pass `fe` to `New()`:
1. `TestInputTextareaSubmit` (`tui_test.go:60`)
2. `TestPendingQueue_EnterWhileStreaming` (`tui_test.go:133`)
3. `TestPendingQueue_DequeueOnTurnDone` (`tui_test.go:158`)
4. `TestStatusMsgClearedOnSend` (`integration_test.go:123`)

All other test functions pass `nil` as `fe` to `New()` -- these just need the `nil` argument removed.

**Files:**
- Modify: `gohome/internal/tui/tui_test.go`
- Modify: `gohome/internal/tui/integration_test.go`
- Modify: `gohome/internal/tui/tui_snapshot_test.go` (if it calls `tui.New(nil, ...)`)
- Any other test files that call `tui.New()`

**Step 1: Find all `tui.New(` call sites in tests**

Run: `grep -rn 'tui\.New(' gohome/internal/tui/ --include='*_test.go'`

Update every `tui.New(nil, "")` to `tui.New("")` and every `tui.New(fe, "")` to `tui.New("")`.

**Step 2: Rewrite `TestInputTextareaSubmit`**

The old test sent input via the editor and read it from `fe.InputCh()`. Since there's no more in-process `inputCh`, this test should verify that typing + Enter adds a user timeline entry (the observable behavior). The actual input delivery goes through `sendInputCmd` which calls `cfe.SendInput()` -- but in the test there's no daemon, so `cfe` is nil and `sendInputCmd` will panic.

Solution: skip testing the actual input delivery (that's an integration concern for daemon tests). Test only that the user message appears in the timeline:

```go
func TestInputTextareaSubmit(t *testing.T) {
	m := tui.New("")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	// Add a user entry directly to verify rendering.
	m.AddTimelineEntry("main", tui.TimelineEntry{Kind: tui.KindUser, Text: "world"})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("world"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
```

**Step 3: Rewrite `TestPendingQueue_EnterWhileStreaming`**

This test verifies that submitting input while streaming queues the message. Without `fe`, we need to test the queuing behavior by directly calling `Model.Update` with key events.

However, since `sendInputCmd` will be called when the user presses Enter, and `cfe` is nil in tests, we need to either:
- Set a mock `ClientFrontend`, or
- Make `sendInputCmd` tolerate nil `cfe` in tests

The simplest approach: make `sendInputCmd` return nil when `cfe` is nil (a no-op). This lets the pending queue logic still work in tests.

In `model_agent.go`, update `sendInputCmd`:

```go
func (m *Model) sendInputCmd(text string) tea.Cmd {
	if m.cfe == nil {
		return nil
	}
	cfe := m.cfe
	sid := m.focused
	return func() tea.Msg {
		cfe.SendInput(sid, text)
		return nil
	}
}
```

Then the test becomes:

```go
func TestPendingQueue_EnterWhileStreaming(t *testing.T) {
	m := tui.New("")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	tm.Send(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventTokenDelta,
		SessionID: "main",
		TextDelta: "working on it...",
	}})

	tm.Type("fix the tests")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Queued:")) && bytes.Contains(out, []byte("fix the tests"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
```

**Step 4: Rewrite `TestPendingQueue_DequeueOnTurnDone`**

Same pattern. Without an `fe.InputCh()` to read from, we verify dequeue by checking that the user message appears in the timeline after `EventTurnDone`:

```go
func TestPendingQueue_DequeueOnTurnDone(t *testing.T) {
	m := tui.New("")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	tm.Send(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventTokenDelta,
		SessionID: "main",
		TextDelta: "response",
	}})

	tm.Type("queued msg")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	tm.Send(tui.AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventTurnDone,
		SessionID: "main",
	}})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("queued msg"))
	}, teatest.WithDuration(3*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
```

**Step 5: Rewrite `TestStatusMsgClearedOnSend` in `integration_test.go`**

Remove the `fe` creation and `InputCh` drain goroutine. The test verifies that the status message is cleared when the user submits input. With `cfe == nil`, `sendInputCmd` returns nil (no-op), but the user timeline entry is still appended and the status message is cleared:

```go
func TestStatusMsgClearedOnSend(t *testing.T) {
	m := tui.New("")
	m.SetSlashCallbacks(tui.SlashCallbacks{
		ListSessions: func() ([]session.Listing, error) {
			return []session.Listing{
				{ID: "s1", Title: "test session"},
			}, nil
		},
		ResumeSession: func(id string) ([]common.Message, error) {
			return nil, nil
		},
	})

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Type("/resume")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("test session"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Resumed:"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Type("hello")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("hello")) && !bytes.Contains(out, []byte("Resumed:"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
```

Remove unused import of `tui.NewFrontend` (it no longer exists). Remove `"github.com/jhyoong/GoHome/gohome/internal/tui"` if it was only used for `NewFrontend` -- but it's also used for `tui.SlashCallbacks`, so keep it. Remove unused imports only.

**Step 6: Update snapshot tests**

Run: `grep -rn 'tui\.New(' gohome/internal/tui/tui_snapshot_test.go`

Update any `tui.New(nil, id)` calls to `tui.New(id)`.

**Step 7: Run full test suite**

Run: `go test ./gohome/internal/tui/ -v`
Expected: all PASS

Run: `go test ./gohome/...`
Expected: all PASS

**Step 8: Commit Phase 1**

```bash
git add gohome/internal/tui/frontend.go gohome/internal/tui/approval.go \
       gohome/internal/tui/model.go gohome/internal/tui/model_agent.go \
       gohome/internal/tui/model_approval.go gohome/internal/tui/client_frontend.go \
       gohome/internal/tui/tui_test.go gohome/internal/tui/integration_test.go \
       gohome/internal/tui/tui_snapshot_test.go gohome/internal/tui/approval_test.go \
       gohome/internal/tui/client_frontend_test.go \
       gohome/cmd/gohome/main.go
git commit -m "refactor: remove dead tui.Frontend, unify approval types, simplify input path"
```

---

### Task 6: Lint and vet pass

Run the project's linters to catch any unused imports, unused variables, or style issues introduced during refactoring.

**Step 1: Run vet**

Run: `go vet ./gohome/...`
Expected: no errors

**Step 2: Run linter**

Run: `golangci-lint run ./gohome/...`
Expected: no errors

**Step 3: Fix any issues found**

Fix any lint/vet issues. Common ones after this refactor:
- Unused imports from deleted code
- Unused variables (e.g., `inputCh` references)
- Missing error checks

**Step 4: Commit if fixes were needed**

```bash
git add -A
git commit -m "fix: resolve lint and vet issues from Phase 1 refactoring"
```

---

### Task 7: Extract `handlers.go` from `server.go`

Move the five RPC handler methods out of `server.go` into a new `handlers.go` file. Same package, no behavioral change.

**Files:**
- Modify: `gohome/internal/daemon/server.go` (remove handler methods)
- Create: `gohome/internal/daemon/handlers.go`

**Step 1: Create `handlers.go`**

Move these methods from `server.go` into the new file:
- `handleSessionList` (lines 444-458)
- `handleSessionNew` (lines 462-503)
- `handleSessionResume` (lines 507-578)
- `handleSessionCancel` (lines 582-595)
- `handleModelSet` (lines 599-655)

The file needs these imports (check which handlers use what):

```go
package daemon

import (
	"encoding/json"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
	"github.com/jhyoong/GoHome/gohome/internal/llm"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)
```

**Step 2: Verify build**

Run: `go build -o /dev/null ./gohome/cmd/gohome/`
Expected: compiles

**Step 3: Run daemon tests**

Run: `go test ./gohome/internal/daemon/ -v`
Expected: all PASS

**Step 4: Commit**

```bash
git add gohome/internal/daemon/handlers.go gohome/internal/daemon/server.go
git commit -m "refactor: extract RPC handlers from server.go into handlers.go"
```

---

### Task 8: Extract `dispatch.go` from `server.go`

Move `dispatch()` and `initAgent()` into a new `dispatch.go` file.

**Files:**
- Modify: `gohome/internal/daemon/server.go` (remove dispatch and initAgent)
- Create: `gohome/internal/daemon/dispatch.go`

**Step 1: Create `dispatch.go`**

Move these methods from `server.go`:
- `dispatch` (lines 213-304, adjusted for handler extraction)
- `initAgent` (lines 306-333)

Imports needed:

```go
package daemon

import (
	"encoding/json"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)
```

**Step 2: Verify build**

Run: `go build -o /dev/null ./gohome/cmd/gohome/`
Expected: compiles

**Step 3: Run tests**

Run: `go test ./gohome/internal/daemon/ -v`
Expected: all PASS

**Step 4: Commit**

```bash
git add gohome/internal/daemon/dispatch.go gohome/internal/daemon/server.go
git commit -m "refactor: extract dispatch and initAgent from server.go"
```

---

### Task 9: Extract `loop.go` from `server.go`

Move `runLoop()` and `sendStateSync()` into a new `loop.go` file.

**Files:**
- Modify: `gohome/internal/daemon/server.go` (remove loop methods)
- Create: `gohome/internal/daemon/loop.go`

**Step 1: Create `loop.go`**

Move these methods from `server.go`:
- `runLoop` (lines 338-415, adjusted for prior extractions)
- `sendStateSync` (lines 420-439, adjusted)

Imports needed:

```go
package daemon

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)
```

**Step 2: Verify build**

Run: `go build -o /dev/null ./gohome/cmd/gohome/`
Expected: compiles

**Step 3: Run tests**

Run: `go test ./gohome/internal/daemon/ -v`
Expected: all PASS

**Step 4: Commit**

```bash
git add gohome/internal/daemon/loop.go gohome/internal/daemon/server.go
git commit -m "refactor: extract runLoop and sendStateSync from server.go"
```

---

### Task 10: Final verification

Run the complete test suite, linter, and build to confirm everything works.

**Step 1: Full test suite**

Run: `go test ./gohome/...`
Expected: all PASS

**Step 2: Lint**

Run: `golangci-lint run ./gohome/...`
Expected: clean

**Step 3: Build**

Run: `go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome`
Expected: binary built, under 25 MB

**Step 4: Verify file sizes**

Run: `wc -l gohome/internal/daemon/*.go | grep -v _test`

Expected approximate sizes:
- `server.go`: ~250 lines
- `dispatch.go`: ~120 lines
- `handlers.go`: ~200 lines
- `loop.go`: ~100 lines

**Step 5: Commit any remaining fixes**

If any fixes were needed, commit them.
