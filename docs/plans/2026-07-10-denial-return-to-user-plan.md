# Denial Return-to-User Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** When a tool call is plain-denied, break out of the agent loop and return control to the user so they can provide corrective input before the LLM retries.

**Architecture:** Add a sentinel error `ErrToolDenied` returned by `Run()` when any tool call in a batch is denied without a steer message. The existing `runLoop` in main.go already loops back to `AwaitUserInput()` after `Run()` returns, so the user naturally gets the editor. Their message lands in history after the denial tool results, giving the LLM both signals.

**Tech Stack:** Go, Bubble Tea TUI

---

### Task 1: Add `ErrToolDenied` sentinel and `EventToolDenied` event kind

**Files:**
- Create: `gohome/internal/agent/errors.go`
- Modify: `gohome/internal/agent/events.go:13-25`
- Modify: `gohome/internal/agent/events_test.go:36-57`

**Step 1: Create the errors file**

Create `gohome/internal/agent/errors.go`:

```go
package agent

import "errors"

var ErrToolDenied = errors.New("tool call denied by user")
```

**Step 2: Add EventToolDenied to events.go**

In `gohome/internal/agent/events.go`, add `EventToolDenied` to the const block (after `EventThinkingDone` on line 24):

```go
EventToolDenied     EventKind = "tool_denied"
```

**Step 3: Add EventToolDenied to the events_test.go constant table**

In `gohome/internal/agent/events_test.go`, add to the `cases` slice in `TestEventKindConstants` (after the `EventThinkingDone` entry):

```go
{EventToolDenied, "tool_denied"},
```

**Step 4: Run tests to verify**

Run: `go test ./gohome/internal/agent/ -run TestEventKindConstants -v`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/agent/errors.go gohome/internal/agent/events.go gohome/internal/agent/events_test.go
git commit -m "feat: add ErrToolDenied sentinel and EventToolDenied event kind"
```

---

### Task 2: Update `dispatchTool` to return a denied flag

**Files:**
- Modify: `gohome/internal/agent/run.go:108-153`

**Step 1: Write the failing test**

In `gohome/internal/agent/run_test.go`, update `TestRun_DeniedTool` (line 159). The test currently expects `Run()` to return `nil`. Change it to expect `ErrToolDenied`:

```go
func TestRun_DeniedTool(t *testing.T) {
	turn1 := []common.StreamEvent{
		{Kind: common.EventToolCallDone, ToolCallID: "tc-deny", ToolName: "fake", InputJSON: `{}`},
		{Kind: common.EventTurnDone, StopReason: "tool_use"},
	}
	// No turn2 needed: Run should return ErrToolDenied before calling Stream again.
	client := &fakeClient{sequences: [][]common.StreamEvent{turn1}}

	executed := false
	tracked := &trackingTool{
		fakeTool: &fakeTool{name: "fake", content: "should-not-run"},
		executed: &executed,
	}
	reg := tools.NewRegistry()
	reg.Register(tracked)

	fe := &fakeRecorder{
		approval: guard.ApprovalDecision{Outcome: ""},
	}
	g := compileDenyGuard(t, fe)
	a, sess := newTestAgentWithGuard(t, client, fe, g, reg)

	err := a.Run(context.Background(), sess)
	if !errors.Is(err, ErrToolDenied) {
		t.Fatalf("Run: got %v, want ErrToolDenied", err)
	}

	if executed {
		t.Error("tool was executed despite being denied")
	}

	// The tool-result message must exist and be IsError.
	var foundToolMsg bool
	for _, msg := range sess.History {
		if msg.Role == common.RoleTool {
			foundToolMsg = true
			for _, b := range msg.Content {
				if !b.IsError {
					t.Errorf("denied tool result block should have IsError=true")
				}
			}
		}
	}
	if !foundToolMsg {
		t.Errorf("no RoleTool message in history after denial")
	}

	// Verify Run only called Stream once (did not proceed to a second turn).
	if client.callCount != 1 {
		t.Errorf("Stream call count: got %d, want 1", client.callCount)
	}

	// Frontend should have seen EventToolDenied.
	var sawDenied bool
	for _, ev := range fe.events {
		if ev.Kind == EventToolDenied {
			sawDenied = true
		}
	}
	if !sawDenied {
		t.Errorf("no EventToolDenied in frontend events")
	}
}
```

Note: add `"errors"` to the import block at the top of `run_test.go`.

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/agent/ -run TestRun_DeniedTool -v`
Expected: FAIL (Run returns nil, not ErrToolDenied)

**Step 3: Update dispatchTool signature and Run loop**

In `gohome/internal/agent/run.go`:

Change `dispatchTool` signature (line 108) to return `denied bool` as a fourth return value:

```go
func (a *Agent) dispatchTool(
	ctx context.Context,
	tctx context.Context,
	sess *session.Session,
	block common.Block,
) (content string, isError bool, elapsed time.Duration, denied bool) {
```

In the denial branch (lines 132-138), set `denied = true` for plain denials only (not steer):

```go
if !dec.Allow {
	if dec.SteerMessage != "" {
		return dec.SteerMessage, true, 0, false
	}
	return "Tool call denied by user.", true, 0, true
}
```

Update the allowed return paths (line 143 unknown tool, line 152 successful execution) to include `false` as the fourth return value:

```go
// Unknown tool (around line 143):
return fmt.Sprintf("unknown tool: %s", block.ToolName), true, 0, false

// Successful execution (around line 152):
return res.Content, res.IsError, elapsed, false
```

In `Run()` (around lines 62-101), update the tool dispatch loop to track denials and return `ErrToolDenied`:

```go
// Dispatch each tool call and collect results.
var resultBlocks []common.Block
var anyDenied bool
for _, block := range toolUseBlocks {
	content, isError, elapsed, denied := a.dispatchTool(ctx, tctx, sess, block)

	if denied {
		anyDenied = true
	}

	// Persist the tool result event.
	if w := a.State.Writer(); w != nil {
		w.Emit(session.ToolResult{
			ToolUseID: block.ToolUseID,
			Content:   content,
			IsError:   isError,
		})
	}

	// Forward to Frontend.
	a.Frontend.Emit(sess.ID, Event{
		Kind:       EventToolResult,
		SessionID:  sess.ID,
		ToolCallID: block.ToolUseID,
		Result: &ToolResult{
			ToolUseID: block.ToolUseID,
			Content:   content,
			IsError:   isError,
			Duration:  elapsed,
		},
	})

	resultBlocks = append(resultBlocks, common.Block{
		Kind:       common.BlockToolResult,
		ToolUseID:  block.ToolUseID,
		ResultText: content,
		IsError:    isError,
	})
}

// Append all results as a single RoleTool message.
sess.History = append(sess.History, common.Message{
	Role:    common.RoleTool,
	Content: resultBlocks,
})

if anyDenied {
	a.Frontend.Emit(sess.ID, Event{
		Kind:      EventToolDenied,
		SessionID: sess.ID,
	})
	return ErrToolDenied
}
```

Also add `"errors"` to the import block if not already present (it is not currently imported in run.go — but `ErrToolDenied` is just a package-level var, so no import needed in run.go itself).

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/agent/ -run TestRun_DeniedTool -v`
Expected: PASS

**Step 5: Run all agent tests to check for regressions**

Run: `go test ./gohome/internal/agent/ -v`
Expected: ALL PASS

**Step 6: Commit**

```bash
git add gohome/internal/agent/run.go gohome/internal/agent/run_test.go
git commit -m "feat: return ErrToolDenied from Run on plain denial"
```

---

### Task 3: Verify deny+steer does NOT return ErrToolDenied

**Files:**
- Modify: `gohome/internal/agent/run_test.go`

**Step 1: Verify the existing TestRun_DenySteer test still passes**

The existing `TestRun_DenySteer` test (line 231) already asserts `Run()` returns `nil` and calls `Stream` twice (meaning the loop continued). This test should still pass without changes since deny+steer sets `denied = false`.

Run: `go test ./gohome/internal/agent/ -run TestRun_DenySteer -v`
Expected: PASS — Run returns nil, client.callCount == 2.

**Step 2: No changes needed**

If the test passes, no code changes are required for this task.

---

### Task 4: Add test for mixed batch (some denied, some approved)

**Files:**
- Modify: `gohome/internal/agent/run_test.go`

**Step 1: Write the test**

Add to `gohome/internal/agent/run_test.go`, after `TestRun_DenySteer`:

```go
// TestRun_MixedBatch verifies that when a batch has one approved and one
// plain-denied tool call, Run returns ErrToolDenied but the approved tool
// still executes and both results appear in history.
func TestRun_MixedBatch(t *testing.T) {
	turn1 := []common.StreamEvent{
		{Kind: common.EventToolCallDone, ToolCallID: "tc-ok", ToolName: "allowed", InputJSON: `{}`},
		{Kind: common.EventToolCallDone, ToolCallID: "tc-no", ToolName: "denied", InputJSON: `{}`},
		{Kind: common.EventTurnDone, StopReason: "tool_use"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{turn1}}

	executedAllowed := false
	executedDenied := false

	reg := tools.NewRegistry()
	reg.Register(&trackingTool{
		fakeTool: &fakeTool{name: "allowed", content: "ok"},
		executed: &executedAllowed,
	})
	reg.Register(&trackingTool{
		fakeTool: &fakeTool{name: "denied", content: "should-not-run"},
		executed: &executedDenied,
	})

	callCount := 0
	fe := &fakeRecorder{
		approvalFunc: func(_ context.Context, req guard.ApprovalRequest) (guard.ApprovalDecision, error) {
			callCount++
			if req.Tool == "allowed" {
				return guard.ApprovalDecision{Outcome: guard.AllowOnce}, nil
			}
			return guard.ApprovalDecision{Outcome: guard.Deny}, nil
		},
	}

	wl, err := guard.Compile(guard.WhitelistFile{}, guard.WhitelistFile{}, "")
	if err != nil {
		t.Fatalf("guard.Compile: %v", err)
	}
	gfe := &guardFE{fe: fe}
	g := guard.NewGuard(wl, gfe)

	a, sess := newTestAgentWithGuard(t, client, fe, g, reg)

	runErr := a.Run(context.Background(), sess)
	if !errors.Is(runErr, ErrToolDenied) {
		t.Fatalf("Run: got %v, want ErrToolDenied", runErr)
	}

	if !executedAllowed {
		t.Error("allowed tool was not executed")
	}
	if executedDenied {
		t.Error("denied tool was executed")
	}

	// History should have: assistant (turn1) + tool results.
	// The RoleTool message should have 2 blocks.
	var toolMsg *common.Message
	for i := range sess.History {
		if sess.History[i].Role == common.RoleTool {
			toolMsg = &sess.History[i]
			break
		}
	}
	if toolMsg == nil {
		t.Fatal("no RoleTool message in history")
	}
	if len(toolMsg.Content) != 2 {
		t.Fatalf("RoleTool blocks: got %d, want 2", len(toolMsg.Content))
	}

	// First block (allowed) should not be error.
	if toolMsg.Content[0].IsError {
		t.Errorf("allowed tool result should not be IsError")
	}
	// Second block (denied) should be error.
	if !toolMsg.Content[1].IsError {
		t.Errorf("denied tool result should be IsError")
	}

	// Run should NOT have called Stream a second time.
	if client.callCount != 1 {
		t.Errorf("Stream call count: got %d, want 1", client.callCount)
	}
}
```

This test requires adding an `approvalFunc` field to `fakeRecorder` to support per-tool approval decisions. Update `fakeRecorder` in `events_test.go`:

```go
type fakeRecorder struct {
	events       []Event
	approval     guard.ApprovalDecision
	approvalFunc func(context.Context, guard.ApprovalRequest) (guard.ApprovalDecision, error)
	approvalErr  error
	userInput    string
	userInputErr error
}

func (f *fakeRecorder) RequestApproval(ctx context.Context, req guard.ApprovalRequest) (guard.ApprovalDecision, error) {
	if f.approvalFunc != nil {
		return f.approvalFunc(ctx, req)
	}
	return f.approval, f.approvalErr
}
```

**Step 2: Run test to verify it passes**

Run: `go test ./gohome/internal/agent/ -run TestRun_MixedBatch -v`
Expected: PASS (the implementation from Task 2 already handles this case)

**Step 3: Run all agent tests**

Run: `go test ./gohome/internal/agent/ -v`
Expected: ALL PASS

**Step 4: Commit**

```bash
git add gohome/internal/agent/run_test.go gohome/internal/agent/events_test.go
git commit -m "test: add mixed batch and per-tool approval support for denial tests"
```

---

### Task 5: Handle `ErrToolDenied` in `runLoop`

**Files:**
- Modify: `gohome/cmd/gohome/main.go:100-161`

**Step 1: Add import and handle the sentinel**

In `gohome/cmd/gohome/main.go`, add `"errors"` to the import block.

In `runLoop`, after `runErr := a.Run(turnCtx, sess)` (line 135), before the existing error handling (lines 137-147), add:

```go
runErr := a.Run(turnCtx, sess)

turnMu.Lock()
*turnCancel = nil
turnMu.Unlock()
cancel()

if errors.Is(runErr, agent.ErrToolDenied) {
	continue
}

if runErr != nil {
	slog.Error("agent run failed", "err", runErr)
	if ctx.Err() != nil {
		return
	}
}
```

This ensures that `ErrToolDenied` causes the loop to continue back to `AwaitUserInput()` without logging an error.

**Step 2: Run the full test suite**

Run: `go test ./gohome/... -v`
Expected: ALL PASS

**Step 3: Commit**

```bash
git add gohome/cmd/gohome/main.go
git commit -m "feat: handle ErrToolDenied in runLoop to return control to user"
```

---

### Task 6: Handle `EventToolDenied` in the TUI

**Files:**
- Modify: `gohome/internal/tui/model_agent.go:12-253`

**Step 1: Add the event handler**

In `gohome/internal/tui/model_agent.go`, inside the `switch ev.Kind` block in `handleAgentEvent`, add a new case before the `case agent.EventUsageUpdated:` block (before line 115):

```go
case agent.EventToolDenied:
	sv.Timeline = append(sv.Timeline, TimelineEntry{
		Kind: KindNotice,
		Text: "Tool call denied. Waiting for your input.",
	})
	sv.InFlight = false
```

Also add `agent.EventToolDenied` to the spinner stop cases on line 210:

```go
case agent.EventTurnDone, agent.EventSessionEnded, agent.EventError, agent.EventToolDenied:
```

**Step 2: Run TUI snapshot tests to check for regressions**

Run: `go test ./gohome/internal/tui/ -run TestSnapshots -v`
Expected: PASS (no snapshot changes since the new event kind only fires on denial, which existing snapshots don't exercise)

**Step 3: Run full test suite**

Run: `go test ./gohome/... -v`
Expected: ALL PASS

**Step 4: Commit**

```bash
git add gohome/internal/tui/model_agent.go
git commit -m "feat: show 'Tool call denied' notice in TUI on EventToolDenied"
```

---

### Task 7: Lint and vet

**Files:** None (verification only)

**Step 1: Run golangci-lint**

Run: `golangci-lint run ./gohome/...`
Expected: No new warnings or errors

**Step 2: Run go vet**

Run: `go vet ./gohome/...`
Expected: Clean

**Step 3: Run full test suite one final time**

Run: `go test ./gohome/...`
Expected: ALL PASS
