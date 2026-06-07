# Thinking Block Consistency Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make thinking/reasoning blocks behave consistently across Anthropic and OpenAI wire protocols for session persistence, TUI display, and resume.

**Architecture:** Three independent changes: (1) fix expansion state defaults in `history_convert.go` and `model.go`, (2) add a validation function in the `session` package that logs warnings for malformed thinking blocks, (3) add cross-protocol tests in `agent/turn_test.go`, `tui/history_convert_test.go`, and `session/validate_test.go`.

**Tech Stack:** Go, standard library `log/slog` for validation warnings, existing test infrastructure (`fakeClient`, `fakeRecorder`).

---

### Task 1: Fix resumed thinking blocks to default collapsed

**Files:**
- Modify: `gohome/internal/tui/history_convert.go:31-35`
- Test: `gohome/internal/tui/history_convert_test.go`

**Step 1: Update existing test to expect `Expanded: false`**

In `history_convert_test.go`, the test `TestHistoryToTimeline_AssistantTextAndThinking` currently does not check `Expanded`. Add an assertion. Also update `TestHistoryToTimeline_FullConversation` similarly.

In `TestHistoryToTimeline_AssistantTextAndThinking` (around line 35), after the existing checks, add:

```go
if got[0].Expanded != false {
    t.Errorf("thinking Expanded = %v, want false", got[0].Expanded)
}
```

In `TestHistoryToTimeline_FullConversation` (around line 133), after the kinds loop, add:

```go
if got[1].Expanded != false {
    t.Errorf("thinking Expanded = %v, want false", got[1].Expanded)
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run "TestHistoryToTimeline_AssistantTextAndThinking|TestHistoryToTimeline_FullConversation" -v`

Expected: FAIL -- `Expanded = true, want false`

**Step 3: Fix `history_convert.go`**

In `gohome/internal/tui/history_convert.go:31-35`, change `Expanded: true` to `Expanded: false`:

```go
case common.BlockThinking:
    entries = append(entries, TimelineEntry{
        Kind:     KindThinking,
        Text:     b.Text,
        Expanded: false,
    })
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run "TestHistoryToTimeline" -v`

Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/tui/history_convert.go gohome/internal/tui/history_convert_test.go
git commit -m "fix: default resumed thinking blocks to collapsed"
```

---

### Task 2: Collapse live thinking blocks on EventThinkingDone

**Files:**
- Modify: `gohome/internal/tui/model.go:295-297`
- Test: `gohome/internal/tui/model_thinking_test.go` (new)

**Step 1: Write the failing test**

Create `gohome/internal/tui/model_thinking_test.go`:

```go
package tui

import (
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
)

func TestHandleAgentEvent_ThinkingDoneCollapsesEntry(t *testing.T) {
	m := New(nil, "sess-1")
	m.winW = 80
	m.winH = 40

	// Simulate a thinking delta arriving (creates an expanded thinking entry).
	m.handleAgentEvent(AgentEventMsg{
		SessionID: "sess-1",
		Ev: agent.Event{
			Kind:          agent.EventThinkingDelta,
			ThinkingDelta: "line one\nline two\nline three",
		},
	})

	sv := m.sessions["sess-1"]
	if len(sv.Timeline) != 1 {
		t.Fatalf("timeline len = %d, want 1", len(sv.Timeline))
	}
	if sv.Timeline[0].Kind != KindThinking {
		t.Fatalf("entry kind = %q, want %q", sv.Timeline[0].Kind, KindThinking)
	}
	if !sv.Timeline[0].Expanded {
		t.Fatal("thinking entry should be expanded during streaming")
	}

	// Simulate thinking done.
	m.handleAgentEvent(AgentEventMsg{
		SessionID: "sess-1",
		Ev:        agent.Event{Kind: agent.EventThinkingDone},
	})

	if sv.Timeline[0].Expanded {
		t.Error("thinking entry should be collapsed after EventThinkingDone")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run "TestHandleAgentEvent_ThinkingDoneCollapsesEntry" -v`

Expected: FAIL -- `thinking entry should be collapsed after EventThinkingDone`

**Step 3: Implement the fix**

In `gohome/internal/tui/model.go`, find the `case agent.EventThinkingDone:` handler (around line 295). Change it from:

```go
case agent.EventThinkingDone:
    // No-op: the thinking entry is already complete.
```

to:

```go
case agent.EventThinkingDone:
    n := len(sv.Timeline)
    for i := n - 1; i >= 0; i-- {
        if sv.Timeline[i].Kind == KindThinking {
            sv.Timeline[i].Expanded = false
            break
        }
    }
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run "TestHandleAgentEvent_ThinkingDoneCollapsesEntry" -v`

Expected: PASS

**Step 5: Run the full TUI test suite to check for regressions**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -v`

Expected: PASS (snapshot tests may need updating if they include thinking entries -- check output)

**Step 6: Commit**

```bash
git add gohome/internal/tui/model.go gohome/internal/tui/model_thinking_test.go
git commit -m "fix: collapse live thinking blocks on EventThinkingDone"
```

---

### Task 3: Add session load validation for thinking blocks

**Files:**
- Create: `gohome/internal/session/validate.go`
- Test: `gohome/internal/session/validate_test.go` (new)

**Step 1: Write the failing tests**

Create `gohome/internal/session/validate_test.go`:

```go
package session

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

func TestValidateHistory_ValidThinkingWithSignature(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	msgs := []common.Message{
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockThinking, Text: "reasoning here", Signature: "sig123"},
			{Kind: common.BlockText, Text: "answer"},
		}},
	}

	ValidateHistory(logger, "sess-1", msgs)

	if buf.Len() != 0 {
		t.Errorf("expected no warnings, got: %s", buf.String())
	}
}

func TestValidateHistory_ValidThinkingWithoutSignature(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	msgs := []common.Message{
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockThinking, Text: "openai reasoning"},
			{Kind: common.BlockText, Text: "answer"},
		}},
	}

	ValidateHistory(logger, "sess-1", msgs)

	if buf.Len() != 0 {
		t.Errorf("expected no warnings for empty signature (OpenAI), got: %s", buf.String())
	}
}

func TestValidateHistory_EmptyTextWarns(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	msgs := []common.Message{
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockThinking, Text: ""},
			{Kind: common.BlockText, Text: "answer"},
		}},
	}

	ValidateHistory(logger, "sess-1", msgs)

	if !strings.Contains(buf.String(), "empty thinking block") {
		t.Errorf("expected warning about empty thinking block, got: %s", buf.String())
	}
}

func TestValidateHistory_MixedBlocksOnlyWarnsInvalid(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	msgs := []common.Message{
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockThinking, Text: "valid thinking"},
			{Kind: common.BlockText, Text: "text"},
		}},
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockThinking, Text: ""},
			{Kind: common.BlockText, Text: "more text"},
		}},
	}

	ValidateHistory(logger, "sess-1", msgs)

	output := buf.String()
	if !strings.Contains(output, "empty thinking block") {
		t.Errorf("expected warning for empty block, got: %s", output)
	}
	// Should only warn once (for the empty block, not the valid one).
	count := strings.Count(output, "empty thinking block")
	if count != 1 {
		t.Errorf("expected 1 warning, got %d", count)
	}
}

func TestValidateHistory_NoThinkingBlocks(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	msgs := []common.Message{
		{Role: common.RoleUser, Content: []common.Block{
			{Kind: common.BlockText, Text: "hello"},
		}},
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockText, Text: "hi"},
		}},
	}

	ValidateHistory(logger, "sess-1", msgs)

	if buf.Len() != 0 {
		t.Errorf("expected no warnings, got: %s", buf.String())
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/session/ -run "TestValidateHistory" -v`

Expected: FAIL -- `ValidateHistory` undefined

**Step 3: Implement `validate.go`**

Create `gohome/internal/session/validate.go`:

```go
package session

import (
	"log/slog"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// ValidateHistory checks loaded session messages for malformed thinking blocks.
// It logs warnings but does not modify or remove any messages.
func ValidateHistory(logger *slog.Logger, sessionID string, msgs []common.Message) {
	for i, msg := range msgs {
		for j, b := range msg.Content {
			if b.Kind != common.BlockThinking {
				continue
			}
			if b.Text == "" {
				logger.Warn("empty thinking block",
					"session", sessionID,
					"message", i,
					"block", j,
				)
			}
		}
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/session/ -run "TestValidateHistory" -v`

Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/session/validate.go gohome/internal/session/validate_test.go
git commit -m "feat: add session load validation for thinking blocks"
```

---

### Task 4: Wire validation into the session load path

**Files:**
- Modify: `gohome/internal/session/load.go:130-131`

**Step 1: Add validation call after history is built**

In `gohome/internal/session/load.go`, after line 130 (`sess.History = history`) and before the return, add:

```go
ValidateHistory(slog.Default(), sess.ID, history)
```

Add `"log/slog"` to the imports if not already present.

**Step 2: Run the full session test suite**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/session/ -v`

Expected: PASS

**Step 3: Commit**

```bash
git add gohome/internal/session/load.go
git commit -m "feat: wire thinking block validation into session load"
```

---

### Task 5: Add cross-protocol persistence tests in agent/turn_test.go

**Files:**
- Modify: `gohome/internal/agent/turn_test.go`

**Step 1: Write test for thinking with signature (Anthropic case)**

Append to `gohome/internal/agent/turn_test.go`:

```go
func TestTurn_ThinkingWithSignature(t *testing.T) {
	usage := &common.Usage{InputTokens: 5, OutputTokens: 3}
	events := []common.StreamEvent{
		{Kind: common.EventThinkingDelta, ThinkingDelta: "deep thought"},
		{Kind: common.EventThinkingDone, Signature: "sig-abc-123"},
		{Kind: common.EventTextDelta, TextDelta: "The answer is 42"},
		{Kind: common.EventTurnDone, StopReason: "end_turn", Usage: usage},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	_, err := a.Turn(context.Background(), sess)
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}

	if len(sess.History) != 1 {
		t.Fatalf("history len = %d, want 1", len(sess.History))
	}
	blocks := sess.History[0].Content
	if len(blocks) != 2 {
		t.Fatalf("blocks len = %d, want 2", len(blocks))
	}
	if blocks[0].Kind != common.BlockThinking {
		t.Errorf("blocks[0].Kind = %v, want BlockThinking", blocks[0].Kind)
	}
	if blocks[0].Text != "deep thought" {
		t.Errorf("thinking text = %q, want %q", blocks[0].Text, "deep thought")
	}
	if blocks[0].Signature != "sig-abc-123" {
		t.Errorf("thinking signature = %q, want %q", blocks[0].Signature, "sig-abc-123")
	}
	if blocks[1].Kind != common.BlockText {
		t.Errorf("blocks[1].Kind = %v, want BlockText", blocks[1].Kind)
	}
}
```

**Step 2: Write test for thinking without signature (OpenAI case)**

Append to `gohome/internal/agent/turn_test.go`:

```go
func TestTurn_ThinkingWithoutSignature(t *testing.T) {
	usage := &common.Usage{InputTokens: 5, OutputTokens: 3}
	events := []common.StreamEvent{
		{Kind: common.EventThinkingDelta, ThinkingDelta: "openai reasoning"},
		{Kind: common.EventThinkingDone},
		{Kind: common.EventTextDelta, TextDelta: "Result"},
		{Kind: common.EventTurnDone, StopReason: "stop", Usage: usage},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	_, err := a.Turn(context.Background(), sess)
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}

	blocks := sess.History[0].Content
	if len(blocks) != 2 {
		t.Fatalf("blocks len = %d, want 2", len(blocks))
	}
	if blocks[0].Kind != common.BlockThinking {
		t.Errorf("blocks[0].Kind = %v, want BlockThinking", blocks[0].Kind)
	}
	if blocks[0].Text != "openai reasoning" {
		t.Errorf("thinking text = %q", blocks[0].Text)
	}
	if blocks[0].Signature != "" {
		t.Errorf("thinking signature = %q, want empty", blocks[0].Signature)
	}
}
```

**Step 3: Run the new tests**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/agent/ -run "TestTurn_ThinkingWith" -v`

Expected: PASS (these test existing behavior, confirming it works)

**Step 4: Commit**

```bash
git add gohome/internal/agent/turn_test.go
git commit -m "test: add cross-protocol thinking block persistence tests"
```

---

### Task 6: Add cross-protocol resume tests in history_convert_test.go

**Files:**
- Modify: `gohome/internal/tui/history_convert_test.go`

**Step 1: Write tests for both protocol origins and edge cases**

Append to `gohome/internal/tui/history_convert_test.go`:

```go
func TestHistoryToTimeline_ThinkingWithSignatureCollapsed(t *testing.T) {
	msgs := []common.Message{
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockThinking, Text: "reasoning", Signature: "sig-xyz"},
			{Kind: common.BlockText, Text: "answer"},
		}},
	}
	got := historyToTimeline(msgs)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Kind != KindThinking {
		t.Errorf("kind = %q, want %q", got[0].Kind, KindThinking)
	}
	if got[0].Expanded {
		t.Error("thinking with signature should be collapsed on resume")
	}
}

func TestHistoryToTimeline_ThinkingWithoutSignatureCollapsed(t *testing.T) {
	msgs := []common.Message{
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockThinking, Text: "openai reasoning"},
			{Kind: common.BlockText, Text: "answer"},
		}},
	}
	got := historyToTimeline(msgs)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Expanded {
		t.Error("thinking without signature should be collapsed on resume")
	}
}

func TestHistoryToTimeline_EmptyThinkingText(t *testing.T) {
	msgs := []common.Message{
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockThinking, Text: ""},
			{Kind: common.BlockText, Text: "answer"},
		}},
	}
	got := historyToTimeline(msgs)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (empty thinking still produces entry)", len(got))
	}
	if got[0].Kind != KindThinking {
		t.Errorf("kind = %q, want %q", got[0].Kind, KindThinking)
	}
	if got[0].Expanded {
		t.Error("empty thinking should be collapsed on resume")
	}
}
```

**Step 2: Run the new tests**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run "TestHistoryToTimeline_Thinking" -v`

Expected: PASS (after Task 1 fix is applied)

**Step 3: Commit**

```bash
git add gohome/internal/tui/history_convert_test.go
git commit -m "test: add cross-protocol thinking block resume tests"
```

---

### Task 7: Run full test suite and verify

**Step 1: Run all tests**

Run: `cd /Users/macminijh/projects/GoHome && go test ./... -v 2>&1 | tail -30`

Expected: All PASS

**Step 2: Run linter if configured**

Run: `cd /Users/macminijh/projects/GoHome && which golangci-lint && golangci-lint run ./... 2>&1 | tail -20 || echo "no linter configured"`

Expected: Clean or no linter

**Step 3: Verify no snapshot test failures**

Run: `cd /Users/macminijh/projects/GoHome && go test ./gohome/internal/tui/ -run "TestSnapshots" -v`

Expected: PASS (thinking blocks in snapshots should not be affected since they are tool/assistant entries, not thinking entries)
