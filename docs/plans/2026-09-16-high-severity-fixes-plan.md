# High-Severity Fixes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix the six high-severity bugs identified in the 2026-09-15 code review (H1-H6).

**Architecture:** Three independent clusters — steering (H1+H2 in agent/run.go and tui/model.go), compaction/replay (H4+H5 in agent/compact.go and session/load.go+events.go), and standalone (H3 in cmd/main.go, H6 in tui/model_keys.go). Each cluster can be implemented and tested independently.

**Tech Stack:** Go 1.25, standard library, Bubble Tea TUI framework

---

## Cluster 1: Steering Fixes (H1 + H2)

### Task 1: Add failing test for multi-tool-call steer corruption (H1)

**Files:**
- Modify: `gohome/internal/agent/run_test.go` (append new test after line 734)

**Step 1: Write the failing test**

Add `TestRun_SteerMultiToolCorruption` to `run_test.go`. This test sends a turn with two tool_use blocks, pre-loads a steer message, and verifies that:
- All tool_use blocks get a tool_result (no orphaned tool_use)
- The steer user message appears AFTER the RoleTool message, not between assistant and tool_result
- The second tool is NOT executed (it was skipped due to steering)

```go
// TestRun_SteerMultiToolCorruption verifies that when a steer arrives between
// two tool calls:
//   - the first tool executes and gets a real result
//   - the second tool gets a synthesized "skipped: user steered" error result
//   - the steer user message appears AFTER the RoleTool message
//   - history is wire-format valid: assistant(tool_use A, B) -> tool(result A, result B) -> user(steer)
func TestRun_SteerMultiToolCorruption(t *testing.T) {
	turn1 := []common.StreamEvent{
		{Kind: common.EventToolCallDone, ToolCallID: "tc1", ToolName: "fake", InputJSON: `{}`},
		{Kind: common.EventToolCallDone, ToolCallID: "tc2", ToolName: "fake", InputJSON: `{}`},
		{Kind: common.EventTurnDone, StopReason: "tool_use"},
	}
	turn2 := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: "steered"},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{turn1, turn2}}
	steerCh := make(chan string, 1)
	steerCh <- "stop and do X instead"

	fe := &fakeRecorder{steerCh: steerCh}
	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "fake", content: "ok"})
	g := compileYoloGuard(t)
	a, sess := newTestAgentWithGuard(t, client, fe, g, reg)

	if err := a.Run(context.Background(), sess); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Validate history order: assistant -> tool -> user(steer) -> assistant
	if len(sess.History) < 4 {
		t.Fatalf("history length = %d, want >= 4", len(sess.History))
	}

	// [0] = assistant (tool_use tc1, tc2)
	if sess.History[0].Role != common.RoleAssistant {
		t.Errorf("history[0].Role = %v, want assistant", sess.History[0].Role)
	}

	// [1] = tool results (must have results for BOTH tc1 and tc2)
	toolMsg := sess.History[1]
	if toolMsg.Role != common.RoleTool {
		t.Fatalf("history[1].Role = %v, want tool", toolMsg.Role)
	}
	if len(toolMsg.Content) != 2 {
		t.Fatalf("tool message has %d blocks, want 2", len(toolMsg.Content))
	}
	// First result should be the real execution result
	if toolMsg.Content[0].ToolUseID != "tc1" {
		t.Errorf("tool block 0 ID = %q, want tc1", toolMsg.Content[0].ToolUseID)
	}
	if toolMsg.Content[0].IsError {
		t.Error("tc1 result should not be error (it was executed)")
	}
	// Second result should be the synthesized skip result
	if toolMsg.Content[1].ToolUseID != "tc2" {
		t.Errorf("tool block 1 ID = %q, want tc2", toolMsg.Content[1].ToolUseID)
	}
	if !toolMsg.Content[1].IsError {
		t.Error("tc2 result should be error (skipped due to steer)")
	}
	if !strings.Contains(toolMsg.Content[1].ResultText, "skipped") {
		t.Errorf("tc2 result text = %q, want to contain 'skipped'", toolMsg.Content[1].ResultText)
	}

	// [2] = user (steer message) — must come AFTER tool results
	if sess.History[2].Role != common.RoleUser {
		t.Errorf("history[2].Role = %v, want user", sess.History[2].Role)
	}
	if sess.History[2].Content[0].Text != "stop and do X instead" {
		t.Errorf("steer text = %q", sess.History[2].Content[0].Text)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/agent/ -run TestRun_SteerMultiToolCorruption -v`
Expected: FAIL — the current code puts the steer user message before tool results and does not synthesize a result for tc2.

### Task 2: Fix drainSteer and the tool-dispatch loop (H1)

**Files:**
- Modify: `gohome/internal/agent/run.go:150-174` (drainSteer)
- Modify: `gohome/internal/agent/run.go:76-135` (tool dispatch loop)

**Step 1: Change drainSteer to not append to history**

In `run.go`, change `drainSteer` so it only returns the steer text without appending to history or emitting a writer event. The caller will handle persistence.

Replace `run.go:150-174` with:

```go
func (a *Agent) drainSteer() string {
	ch := a.Frontend.SteerCh()
	if ch == nil {
		return ""
	}
	select {
	case steer := <-ch:
		return steer
	default:
		return ""
	}
}
```

**Step 2: Update the tool-dispatch loop to synthesize skip results and append steer after tool results**

In `run.go`, update the tool-dispatch section (lines 76-135). The key changes:

1. When `drainSteer` returns a message mid-loop, save it but keep looping through remaining `toolUseBlocks` to synthesize skip results for each.
2. After the loop, append the complete `resultBlocks` as a `RoleTool` message first.
3. Then, if there was a steer, append it as a user message and persist both.

Replace lines 76-135 with:

```go
		// Dispatch each tool call, checking for mid-turn steering between calls.
		var resultBlocks []common.Block
		var anyDenied bool
		var steerText string
		for i, block := range toolUseBlocks {
			// Check for steering before each tool call (except the first).
			if i > 0 && steerText == "" {
				steerText = a.drainSteer()
			}

			// If steered, synthesize skip results for remaining tool calls.
			if steerText != "" {
				resultBlocks = append(resultBlocks, common.Block{
					Kind:       common.BlockToolResult,
					ToolUseID:  block.ToolUseID,
					ResultText: "skipped: user steered",
					IsError:    true,
				})
				continue
			}

			content, isError, elapsed, denied := a.dispatchTool(ctx, tctx, sess, block)

			if denied {
				anyDenied = true
			}

			if w := a.State.Writer(); w != nil {
				w.Emit(session.ToolResult{
					ToolUseID: block.ToolUseID,
					Content:   content,
					IsError:   isError,
				})
			}

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

		// Append collected results as a single RoleTool message.
		if len(resultBlocks) > 0 {
			sess.History = append(sess.History, common.Message{
				Role:    common.RoleTool,
				Content: resultBlocks,
			})
		}

		// Append steer as a user message AFTER tool results.
		if steerText == "" {
			// Final steer check after all tool calls.
			steerText = a.drainSteer()
		}
		if steerText != "" {
			steerMsg := common.Message{
				Role:    common.RoleUser,
				Content: []common.Block{{Kind: common.BlockText, Text: steerText}},
			}
			sess.History = append(sess.History, steerMsg)
			if w := a.State.Writer(); w != nil {
				w.Emit(session.UserMessage{
					Content: []common.Block{{Kind: common.BlockText, Text: steerText}},
				})
			}
		}

		if anyDenied {
			a.Frontend.Emit(sess.ID, Event{
				Kind:      EventToolDenied,
				SessionID: sess.ID,
			})
			return ErrToolDenied
		}
```

**Step 3: Run test to verify it passes**

Run: `go test ./gohome/internal/agent/ -run TestRun_SteerMultiToolCorruption -v`
Expected: PASS

**Step 4: Run all existing agent tests to check for regressions**

Run: `go test ./gohome/internal/agent/ -v`
Expected: All tests PASS, including `TestRun_SteerInjection` (single tool call still works).

**Step 5: Commit**

```bash
git add gohome/internal/agent/run.go gohome/internal/agent/run_test.go
git commit -m "fix(agent): prevent steer from corrupting history with multi-tool turns (H1)"
```

### Task 3: Add failing test for text-only steer delay (H2)

**Files:**
- Modify: `gohome/internal/agent/run_test.go`

**Step 1: Write the failing test**

Add `TestRun_SteerDuringTextOnlyTurn` that sends a text-only turn (no tool_use) with a pre-loaded steer message, and verifies the steer is consumed as the next user message (the loop continues for another turn).

```go
// TestRun_SteerDuringTextOnlyTurn verifies that a steer message sent during a
// text-only turn (no tool calls) is drained and used as the next user input
// rather than being silently delayed.
func TestRun_SteerDuringTextOnlyTurn(t *testing.T) {
	// Turn 1: text only, no tool calls.
	turn1 := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: "here is my answer"},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	// Turn 2: response to the steer.
	turn2 := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: "ok redirecting"},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{turn1, turn2}}

	steerCh := make(chan string, 1)
	steerCh <- "actually do Y instead"

	fe := &fakeRecorder{steerCh: steerCh}
	reg := tools.NewRegistry()
	g := compileYoloGuard(t)
	a, sess := newTestAgentWithGuard(t, client, fe, g, reg)

	if err := a.Run(context.Background(), sess); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The steer should have been consumed and driven a second turn.
	if client.callCount != 2 {
		t.Errorf("Stream call count = %d, want 2 (steer should trigger second turn)", client.callCount)
	}

	// The steer message should be in history as a user message.
	var found bool
	for _, msg := range sess.History {
		if msg.Role == common.RoleUser {
			for _, b := range msg.Content {
				if b.Text == "actually do Y instead" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("steer message not found in session history")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/agent/ -run TestRun_SteerDuringTextOnlyTurn -v`
Expected: FAIL — `client.callCount` is 1 (steer was not consumed).

### Task 4: Fix the no-tool-calls path to drain steerCh (H2)

**Files:**
- Modify: `gohome/internal/agent/run.go:71-74`

**Step 1: Replace the no-tool-calls early return with a steer drain**

Replace lines 71-74:

```go
		// No tool calls: check for a pending steer before ending the loop.
		if len(toolUseBlocks) == 0 {
			steerText := a.drainSteer()
			if steerText != "" {
				steerMsg := common.Message{
					Role:    common.RoleUser,
					Content: []common.Block{{Kind: common.BlockText, Text: steerText}},
				}
				sess.History = append(sess.History, steerMsg)
				if w := a.State.Writer(); w != nil {
					w.Emit(session.UserMessage{
						Content: []common.Block{{Kind: common.BlockText, Text: steerText}},
					})
				}
				continue // loop back for another Turn with the steer as input
			}
			return nil
		}
```

**Step 2: Run test to verify it passes**

Run: `go test ./gohome/internal/agent/ -run TestRun_SteerDuringTextOnlyTurn -v`
Expected: PASS

**Step 3: Run all agent tests**

Run: `go test ./gohome/internal/agent/ -v`
Expected: All PASS

### Task 5: Drain steerCh on cancel (H2 part 2)

**Files:**
- Modify: `gohome/internal/tui/model.go:374-388`

**Step 1: Add steerCh drain to cancelFocusedSessionWith**

After line 383 (`m.pendingMessages = m.pendingMessages[:0]`), add:

```go
		// Drain steerCh so a stale steer doesn't resurface on the next run.
		if sv.SteerCh != nil {
			select {
			case <-sv.SteerCh:
			default:
			}
		}
```

Note: The exact field name for the steer channel on the session view may vary. Check the `sessionView` struct to find the correct field name (likely `SteerCh` or accessed via a method). The drain pattern is `select { case <-ch: default: }`.

**Step 2: Run TUI tests**

Run: `go test ./gohome/internal/tui/ -v`
Expected: All PASS

**Step 3: Commit**

```bash
git add gohome/internal/agent/run.go gohome/internal/agent/run_test.go gohome/internal/tui/model.go
git commit -m "fix(agent): drain steer channel on text-only turns and cancel (H2)"
```

---

## Cluster 2: Compaction and Replay Fixes (H4 + H5)

### Task 6: Add failing test for tool blocks in compaction request (H4.1)

**Files:**
- Modify: `gohome/internal/agent/compact_test.go`

**Step 1: Write the failing test**

Add `TestCompact_StripsToolBlocks` that sets up a history containing `tool_use` and `tool_result` blocks in the summarisable region, and verifies the request sent to the LLM does not contain tool blocks (it should contain their text equivalents instead).

```go
// TestCompact_StripsToolBlocks verifies that tool_use and tool_result blocks
// in the summarisable region are converted to plain text before being sent
// to the LLM for summarisation.
func TestCompact_StripsToolBlocks(t *testing.T) {
	summaryText := "summary with tools"
	events := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: summaryText},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}

	var capturedReq common.Request
	client := &capturingClient{
		fakeClient: &fakeClient{sequences: [][]common.StreamEvent{events}},
		onStream:   func(req common.Request) { capturedReq = req },
	}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)
	a.State = NewSessionState(sess, a.State.Writer(), client)

	// 8 messages with tool blocks in the middle (summarisable region)
	sess.History = []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "first"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply"}}},
		// These are in the summarisable region:
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "run ls"}}},
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockText, Text: "calling tool"},
			{Kind: common.BlockToolUse, ToolUseID: "tc1", ToolName: "shell", InputJSON: `{"command":"ls"}`},
		}},
		{Role: common.RoleTool, Content: []common.Block{
			{Kind: common.BlockToolResult, ToolUseID: "tc1", ResultText: "file1.go"},
		}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "done"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "recent"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "recent reply"}}},
	}

	if err := a.compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}

	// Verify no tool blocks in the request messages
	for i, msg := range capturedReq.Messages {
		for j, block := range msg.Content {
			if block.Kind == common.BlockToolUse {
				t.Errorf("request message[%d].block[%d] is tool_use, should be stripped", i, j)
			}
			if block.Kind == common.BlockToolResult {
				t.Errorf("request message[%d].block[%d] is tool_result, should be stripped", i, j)
			}
		}
		if msg.Role == common.RoleTool {
			t.Errorf("request message[%d] has RoleTool, should be converted", i)
		}
	}
}
```

You will also need to add a `capturingClient` type if one does not exist:

```go
type capturingClient struct {
	*fakeClient
	onStream func(common.Request)
}

func (c *capturingClient) Stream(ctx context.Context, req common.Request) (<-chan common.StreamEvent, error) {
	if c.onStream != nil {
		c.onStream(req)
	}
	return c.fakeClient.Stream(ctx, req)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/agent/ -run TestCompact_StripsToolBlocks -v`
Expected: FAIL — tool blocks are sent as-is.

### Task 7: Implement stripToolBlocks and MaxTokens fallback (H4.1 + H4.2)

**Files:**
- Modify: `gohome/internal/agent/compact.go:55-96`

**Step 1: Add the stripToolBlocks helper**

Add this function to `compact.go` (before or after `compact`):

```go
// stripToolBlocks converts tool_use and tool_result blocks to plain text
// and converts RoleTool messages to RoleUser so the summarisation request
// does not contain any tool blocks (which require a tools definition).
func stripToolBlocks(msgs []common.Message) []common.Message {
	out := make([]common.Message, 0, len(msgs))
	for _, msg := range msgs {
		role := msg.Role
		if role == common.RoleTool {
			role = common.RoleUser
		}
		var blocks []common.Block
		for _, b := range msg.Content {
			switch b.Kind {
			case common.BlockToolUse:
				blocks = append(blocks, common.Block{
					Kind: common.BlockText,
					Text: fmt.Sprintf("[Tool call: %s(%s)]", b.ToolName, b.InputJSON),
				})
			case common.BlockToolResult:
				blocks = append(blocks, common.Block{
					Kind: common.BlockText,
					Text: fmt.Sprintf("[Result: %s]", b.ResultText),
				})
			default:
				blocks = append(blocks, b)
			}
		}
		out = append(out, common.Message{Role: role, Content: blocks})
	}
	return out
}
```

**Step 2: Update compact() to use stripToolBlocks and apply the MaxTokens fallback**

In `compact.go`, replace lines 91-96 with:

```go
	maxTokens := 4096
	if a.MaxTokens > 0 {
		maxTokens = a.MaxTokens
	}

	req := common.Request{
		Model:     sess.Model,
		System:    prompt,
		Messages:  stripToolBlocks(oldMessages),
		MaxTokens: maxTokens,
	}
```

**Step 3: Run test to verify it passes**

Run: `go test ./gohome/internal/agent/ -run TestCompact_StripsToolBlocks -v`
Expected: PASS

**Step 4: Run all compact tests**

Run: `go test ./gohome/internal/agent/ -run TestCompact -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add gohome/internal/agent/compact.go gohome/internal/agent/compact_test.go
git commit -m "fix(compact): strip tool blocks and apply MaxTokens fallback (H4.1, H4.2)"
```

### Task 8: Add failing test for unsafe split points (H4.3)

**Files:**
- Modify: `gohome/internal/agent/compact_test.go`

**Step 1: Write the failing test**

Add `TestCompact_PrefixEndsWithToolUse` that places a `tool_use`-only assistant message at `History[1]` (the end of the stable prefix). After compaction the history should not have a tool_use without a matching tool_result.

```go
// TestCompact_PrefixEndsWithToolUse verifies that when the stable prefix ends
// with an assistant message containing tool_use blocks, the prefix boundary is
// adjusted so the tool_use and its result stay together.
func TestCompact_PrefixEndsWithToolUse(t *testing.T) {
	summaryText := "summary"
	events := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: summaryText},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	// History where prefix[1] is an assistant with tool_use
	sess.History = []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "do something"}}},
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockToolUse, ToolUseID: "tc1", ToolName: "shell", InputJSON: `{"command":"ls"}`},
		}},
		{Role: common.RoleTool, Content: []common.Block{
			{Kind: common.BlockToolResult, ToolUseID: "tc1", ResultText: "files"},
		}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "q2"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "a2"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "q3"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "a3"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "q4"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "a4"}}},
	}

	if err := a.compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}

	// Verify no orphaned tool_use (every tool_use has a subsequent tool_result)
	for i, msg := range sess.History {
		if msg.Role == common.RoleAssistant {
			for _, b := range msg.Content {
				if b.Kind == common.BlockToolUse {
					// Next message must be RoleTool with matching ID
					if i+1 >= len(sess.History) || sess.History[i+1].Role != common.RoleTool {
						t.Errorf("tool_use at history[%d] has no following tool_result", i)
					}
				}
			}
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/agent/ -run TestCompact_PrefixEndsWithToolUse -v`
Expected: FAIL — the prefix includes the tool_use assistant but the tool_result is in oldMessages.

### Task 9: Fix split-point safety in compact (H4.3)

**Files:**
- Modify: `gohome/internal/agent/compact.go:55-73`

**Step 1: Extend the split-point guards**

Replace lines 55-73 with:

```go
	keepCount := 4
	prefixCount := 2

	// Adjust prefixCount: if the last message of the prefix is an assistant
	// with tool_use blocks, shrink the prefix so it doesn't orphan the tool_use.
	if prefixCount > 0 && prefixCount < len(sess.History) {
		last := sess.History[prefixCount-1]
		if last.Role == common.RoleAssistant {
			hasToolUse := false
			for _, b := range last.Content {
				if b.Kind == common.BlockToolUse {
					hasToolUse = true
					break
				}
			}
			if hasToolUse {
				prefixCount = 0
			}
		}
	}

	// Need at least: prefix + 1 message to summarize + keepCount recent
	minRequired := prefixCount + 1 + keepCount
	if len(sess.History) < minRequired {
		return nil
	}

	splitIdx := len(sess.History) - keepCount

	// Don't split a tool-use/tool-result pair: if splitIdx lands on a
	// RoleTool message, include its preceding assistant message too.
	if splitIdx > 0 && sess.History[splitIdx].Role == common.RoleTool {
		splitIdx--
	}

	// Also check the start of oldMessages: if it begins with RoleTool,
	// push the split forward to exclude the orphaned result.
	if prefixCount < len(sess.History) && sess.History[prefixCount].Role == common.RoleTool {
		prefixCount++
	}

	if splitIdx <= prefixCount {
		return nil
	}
```

**Step 2: Run test to verify it passes**

Run: `go test ./gohome/internal/agent/ -run TestCompact_PrefixEndsWithToolUse -v`
Expected: PASS

**Step 3: Run all compact tests**

Run: `go test ./gohome/internal/agent/ -run TestCompact -v`
Expected: All PASS

**Step 4: Commit**

```bash
git add gohome/internal/agent/compact.go gohome/internal/agent/compact_test.go
git commit -m "fix(compact): guard against unsafe prefix and split boundaries (H4.3)"
```

### Task 10: Add failing test for compaction replay losing retained messages (H5)

**Files:**
- Modify: `gohome/internal/session/load_test.go`

**Step 1: Write the failing test**

Add `TestLoad_CompactionRetainsFullHistory` that writes a compaction event with a `History` field containing the full post-compaction snapshot. After `Load`, the history must match the snapshot, not just the summary.

```go
// TestLoad_CompactionRetainsFullHistory verifies that after a compaction event,
// Load restores the full history snapshot (prefix + summary + recent), not
// just the summary.
func TestLoad_CompactionRetainsFullHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	w, err := OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}

	w.Emit(SessionStart{ID: "sess-h5", CWD: "/tmp", Model: "m"})

	// Pre-compaction messages (will be discarded by compaction event)
	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "old"}}})
	w.Emit(AssistantMessage{Content: []common.Block{{Kind: common.BlockText, Text: "old reply"}}})

	// Compaction event with full history snapshot
	w.Emit(Compaction{
		BeforeTokens: 50000,
		AfterTokens:  10000,
		Summary:      "conversation summary",
		History: []common.Message{
			{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "first prompt"}}},
			{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "first reply"}}},
			{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: CompactSummaryPrefix + "conversation summary"}}},
			{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "recent q"}}},
			{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "recent a"}}},
		},
	})

	// Post-compaction messages
	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "new"}}})
	w.Emit(AssistantMessage{Content: []common.Block{{Kind: common.BlockText, Text: "new reply"}}})

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, history, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Should be: 5 snapshot messages + 2 post-compaction = 7
	if len(history) != 7 {
		t.Fatalf("len(history) = %d, want 7", len(history))
	}

	// First message should be from the snapshot, not the summary-only fallback
	if history[0].Content[0].Text != "first prompt" {
		t.Errorf("history[0] = %q, want 'first prompt'", history[0].Content[0].Text)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/session/ -run TestLoad_CompactionRetainsFullHistory -v`
Expected: FAIL — the current `Compaction` struct has no `History` field, or `Load` ignores it.

### Task 11: Add History field to Compaction event and update Load (H5)

**Files:**
- Modify: `gohome/internal/session/events.go:70-74` (Compaction struct)
- Modify: `gohome/internal/session/load.go:120-134` (compaction handling)
- Modify: `gohome/internal/agent/compact.go:133-139` (emit with history)

**Step 1: Add History field to Compaction struct**

In `events.go`, update the `Compaction` struct:

```go
type Compaction struct {
	BeforeTokens int              `json:"beforeTokens"`
	AfterTokens  int              `json:"afterTokens"`
	Summary      string           `json:"summary"`
	History      []common.Message `json:"history,omitempty"`
}
```

**Step 2: Update Load to use snapshot when available**

In `load.go`, replace lines 120-134 with:

```go
		case "compaction":
			var ev struct {
				Summary string           `json:"summary"`
				History []common.Message `json:"history"`
			}
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				continue
			}
			if len(ev.History) > 0 {
				history = make([]common.Message, len(ev.History))
				copy(history, ev.History)
			} else {
				history = []common.Message{
					{
						Role: common.RoleUser,
						Content: []common.Block{
							{Kind: common.BlockText, Text: CompactSummaryPrefix + ev.Summary},
						},
					},
				}
			}
```

**Step 3: Update compact() to persist the full history snapshot**

In `compact.go`, replace lines 133-139 with:

```go
	if w := a.State.Writer(); w != nil {
		w.Emit(session.Compaction{
			BeforeTokens: beforeTokens,
			AfterTokens:  afterTokens,
			Summary:      summary,
			History:      sess.History,
		})
	}
```

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/session/ -run TestLoad_CompactionRetainsFullHistory -v`
Expected: PASS

**Step 5: Run all session and compact tests**

Run: `go test ./gohome/internal/session/ -v && go test ./gohome/internal/agent/ -run TestCompact -v`
Expected: All PASS

**Step 6: Commit**

```bash
git add gohome/internal/session/events.go gohome/internal/session/load.go gohome/internal/session/load_test.go gohome/internal/agent/compact.go
git commit -m "fix(session): persist full history snapshot in compaction event (H5)"
```

---

## Cluster 3: Standalone Fixes (H3 + H6)

### Task 12: Fix headless --prompt - requiring --yolo (H3)

**Files:**
- Modify: `gohome/cmd/gohome/main.go:210`

**Step 1: Change the --yolo condition**

Replace line 210:

```go
	if *prompt != "" && !*yolo {
```

This removes the `&& *prompt != "-"` exception, so `--prompt -` now also requires `--yolo`.

**Step 2: Verify the change builds**

Run: `go build -o /dev/null ./gohome/cmd/gohome`
Expected: Build succeeds.

**Step 3: Verify existing tests pass**

Run: `go test ./gohome/... -count=1`
Expected: All PASS

**Step 4: Commit**

```bash
git add gohome/cmd/gohome/main.go
git commit -m "fix(headless): require --yolo for --prompt - mode (H3)"
```

### Task 13: Rebind copy from 'c' to Ctrl+Y (H6)

**Files:**
- Modify: `gohome/internal/tui/model_keys.go:203-215`
- Modify: `README.md:237` (keybinding table)

**Step 1: Change the keybinding**

In `model_keys.go`, replace the `c` key check at line 204:

```go
			if msg.Type == tea.KeyCtrlY {
```

This changes `keyRune(msg) == 'c'` to `msg.Type == tea.KeyCtrlY`. The `c` rune will now fall through to the editor and be inserted normally.

**Step 2: Update the README keybinding table**

In `README.md`, add a row to the keybinding table (after line 236, the `Esc` row):

```markdown
| `Ctrl+Y` | Copy selected timeline entry to clipboard (when cursor is active) |
```

**Step 3: Run TUI snapshot tests**

Run: `go test ./gohome/internal/tui/ -run TestSnapshots -v`
Expected: PASS (no snapshot changes expected since the test inputs don't send `c` or `Ctrl+Y`).

**Step 4: Commit**

```bash
git add gohome/internal/tui/model_keys.go README.md
git commit -m "fix(tui): rebind timeline copy from 'c' to Ctrl+Y (H6)"
```

---

## Final Verification

### Task 14: Run full test suite and vet/lint

**Step 1: Run all tests**

Run: `go test ./gohome/... -count=1`
Expected: All PASS

**Step 2: Run vet and lint**

Run: `go vet ./gohome/... && golangci-lint run ./gohome/...`
Expected: No errors or warnings

**Step 3: Build the binary**

Run: `go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome`
Expected: Build succeeds
