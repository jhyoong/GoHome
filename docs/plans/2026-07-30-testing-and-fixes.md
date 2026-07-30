# Testing & Fixes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix the context compaction cache bug, fix the denylist DenyInfo propagation bug, add E2E tests for headless workflows and compaction, and document Windows Defender false positives.

**Architecture:** Four independent work items that can be implemented in any order. The denylist fix and compaction fix are small targeted changes to `agent/run.go` and `agent/compact.go` respectively. E2E tests extend the existing `test/e2e/smoke_test.go` using the `e2e` build tag and real LLM endpoints. The Windows Defender doc is a standalone markdown file.

**Tech Stack:** Go 1.25, existing `agent`, `guard`, `headless`, `session` packages, `go test -tags e2e`

---

### Task 1: Fix denylist DenyInfo propagation bug

**Files:**
- Modify: `gohome/internal/agent/run.go:156-161`

**Step 1: Write the failing test**

Add to `gohome/internal/agent/run_test.go`:

```go
// compileDenylistGuard returns a Guard with a real Denylist and yolo enabled.
// This proves denylist overrides yolo.
func compileDenylistGuard(t *testing.T, patterns []string) *guard.Guard {
	t.Helper()
	dl, err := guard.CompileDenylist(guard.DenylistFile{Shell: patterns})
	if err != nil {
		t.Fatalf("CompileDenylist: %v", err)
	}
	wl, err := guard.Compile(guard.WhitelistFile{}, guard.WhitelistFile{}, "")
	if err != nil {
		t.Fatalf("guard.Compile: %v", err)
	}
	g := guard.NewGuard(wl, nil, dl)
	g.SetYolo(true)
	return g
}

// TestRun_DenylistBlocksShellCommand verifies that when the denylist blocks a
// shell command:
//   - the tool is NOT executed
//   - the tool result contains the DenyInfo message (not "denied by user")
//   - Run does NOT return ErrToolDenied (agent can self-correct)
//   - the agent continues to a second turn
func TestRun_DenylistBlocksShellCommand(t *testing.T) {
	turn1 := []common.StreamEvent{
		{Kind: common.EventToolCallDone, ToolCallID: "tc-deny", ToolName: "shell", InputJSON: `{"command":"rm -rf /tmp/foo"}`},
		{Kind: common.EventTurnDone, StopReason: "tool_use"},
	}
	turn2 := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: "ok I will not do that"},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{turn1, turn2}}

	executed := false
	tracked := &trackingTool{
		fakeTool: &fakeTool{name: "shell", content: "should-not-run"},
		executed: &executed,
	}
	reg := tools.NewRegistry()
	reg.Register(tracked)

	fe := &fakeRecorder{}
	g := compileDenylistGuard(t, []string{"rm -rf"})
	a, sess := newTestAgentWithGuard(t, client, fe, g, reg)

	err := a.Run(context.Background(), sess)
	if err != nil {
		t.Fatalf("Run should succeed (denylist should not halt agent): %v", err)
	}

	if executed {
		t.Error("shell tool was executed despite being denylisted")
	}

	// The tool result should contain the denylist info, not "denied by user".
	var foundToolResult bool
	for _, msg := range sess.History {
		if msg.Role != common.RoleTool {
			continue
		}
		for _, b := range msg.Content {
			if b.ToolUseID == "tc-deny" {
				foundToolResult = true
				if !b.IsError {
					t.Error("denylist result should have IsError=true")
				}
				if !strings.Contains(b.ResultText, "denylist") {
					t.Errorf("result text should mention denylist, got: %q", b.ResultText)
				}
				if strings.Contains(b.ResultText, "denied by user") {
					t.Errorf("result text should NOT say 'denied by user', got: %q", b.ResultText)
				}
			}
		}
	}
	if !foundToolResult {
		t.Error("no tool result found for tc-deny")
	}

	// Run should have called Stream twice (agent continued to turn 2).
	if client.callCount != 2 {
		t.Errorf("Stream call count: got %d, want 2", client.callCount)
	}

	// Frontend should NOT have seen EventToolDenied.
	for _, ev := range fe.events {
		if ev.Kind == EventToolDenied {
			t.Error("EventToolDenied should not be emitted for denylist rejections")
		}
	}
}
```

Note: add `"strings"` to the import block in `run_test.go`.

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/agent/ -run TestRun_DenylistBlocksShellCommand -v`

Expected: FAIL — the test expects Run to return nil, but current code returns `ErrToolDenied`. The tool result text says "denied by user" instead of mentioning "denylist".

**Step 3: Fix dispatchTool in run.go**

In `gohome/internal/agent/run.go`, replace lines 156-161:

```go
	if !dec.Allow {
		if dec.SteerMessage != "" {
			return dec.SteerMessage, true, 0, false
		}
		return "Tool call denied by user.", true, 0, true
	}
```

With:

```go
	if !dec.Allow {
		if dec.DenyInfo != "" {
			return dec.DenyInfo, true, 0, false
		}
		if dec.SteerMessage != "" {
			return dec.SteerMessage, true, 0, false
		}
		return "Tool call denied by user.", true, 0, true
	}
```

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/agent/ -run TestRun_DenylistBlocksShellCommand -v`

Expected: PASS

**Step 5: Run the full agent test suite**

Run: `go test ./gohome/internal/agent/ -v`

Expected: All existing tests still pass. The existing `TestRun_DeniedTool` (user denial) should be unaffected because user denials have empty `DenyInfo`.

**Step 6: Commit**

```bash
git add gohome/internal/agent/run.go gohome/internal/agent/run_test.go
git commit -m "fix(guard): surface DenyInfo to LLM instead of halting on denylist rejection"
```

---

### Task 2: Add remaining denylist agent-level tests

**Files:**
- Modify: `gohome/internal/agent/run_test.go`

**Step 1: Add TestRun_DenylistNonShellPassthrough**

```go
// TestRun_DenylistNonShellPassthrough verifies that the denylist only checks
// shell tool calls. A non-shell tool should pass through even if the denylist
// has patterns that would match its input.
func TestRun_DenylistNonShellPassthrough(t *testing.T) {
	turn1 := []common.StreamEvent{
		{Kind: common.EventToolCallDone, ToolCallID: "tc-ok", ToolName: "fake", InputJSON: `{}`},
		{Kind: common.EventTurnDone, StopReason: "tool_use"},
	}
	turn2 := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: "done"},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{turn1, turn2}}

	executed := false
	tracked := &trackingTool{
		fakeTool: &fakeTool{name: "fake", content: "tool ran"},
		executed: &executed,
	}
	reg := tools.NewRegistry()
	reg.Register(tracked)

	fe := &fakeRecorder{}
	g := compileDenylistGuard(t, []string{"rm -rf"})
	a, sess := newTestAgentWithGuard(t, client, fe, g, reg)

	if err := a.Run(context.Background(), sess); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !executed {
		t.Error("non-shell tool should have been executed despite denylist")
	}
}
```

**Step 2: Add TestRun_DenylistAgentSelfCorrects**

```go
// TestRun_DenylistAgentSelfCorrects verifies the full self-correction flow:
// turn 1 requests a denylisted command (blocked), turn 2 requests a safe
// command (allowed), turn 3 ends. The agent completes successfully.
func TestRun_DenylistAgentSelfCorrects(t *testing.T) {
	turn1 := []common.StreamEvent{
		{Kind: common.EventToolCallDone, ToolCallID: "tc-bad", ToolName: "shell", InputJSON: `{"command":"rm -rf /tmp"}`},
		{Kind: common.EventTurnDone, StopReason: "tool_use"},
	}
	turn2 := []common.StreamEvent{
		{Kind: common.EventToolCallDone, ToolCallID: "tc-good", ToolName: "shell", InputJSON: `{"command":"ls /tmp"}`},
		{Kind: common.EventTurnDone, StopReason: "tool_use"},
	}
	turn3 := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: "here are the files"},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{turn1, turn2, turn3}}

	execCount := 0
	shellTool := &fakeTool{name: "shell", content: "file1.txt\nfile2.txt"}
	tracked := &trackingTool{
		fakeTool: shellTool,
		executed: new(bool),
	}
	// Override Execute to count calls.
	reg := tools.NewRegistry()
	reg.Register(&countingTool{
		fakeTool: shellTool,
		count:    &execCount,
	})

	fe := &fakeRecorder{}
	g := compileDenylistGuard(t, []string{"rm -rf"})
	a, sess := newTestAgentWithGuard(t, client, fe, g, reg)

	if err := a.Run(context.Background(), sess); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The shell tool should have been executed exactly once (the safe command).
	if execCount != 1 {
		t.Errorf("shell tool execution count: got %d, want 1", execCount)
	}

	// Stream should have been called 3 times.
	if client.callCount != 3 {
		t.Errorf("Stream call count: got %d, want 3", client.callCount)
	}
}

// countingTool wraps fakeTool and counts Execute calls.
type countingTool struct {
	*fakeTool
	count *int
}

func (c *countingTool) Execute(ctx context.Context, in json.RawMessage, sink tools.ProgressSink) (tools.Result, error) {
	*c.count++
	return c.fakeTool.Execute(ctx, in, sink)
}
```

**Step 3: Run all new tests**

Run: `go test ./gohome/internal/agent/ -run "TestRun_Denylist" -v`

Expected: All three denylist tests pass.

**Step 4: Run full test suite**

Run: `go test ./gohome/internal/agent/ -v`

Expected: All tests pass.

**Step 5: Commit**

```bash
git add gohome/internal/agent/run_test.go
git commit -m "test(guard): add agent-level tests for denylist rejection flow"
```

---

### Task 3: Fix context auto compaction — partial compaction

**Files:**
- Modify: `gohome/internal/agent/compact.go:41-95`
- Modify: `gohome/internal/agent/compact_test.go`

**Step 1: Update TestCompact_ReplacesHistory to expect partial compaction**

The existing test has 4 messages and should now keep the last ~2 messages (the most recent turn pair) and summarize the first 2. Update in `compact_test.go`:

```go
func TestCompact_KeepsRecentMessages(t *testing.T) {
	summaryText := "This is the compacted summary."
	events := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: summaryText},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	sess.History = []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "first"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "second"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply2"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "third"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply3"}}},
	}

	if err := a.compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}

	// Should be: summary + last 4 messages (2 turns kept).
	if len(sess.History) < 3 {
		t.Fatalf("len(sess.History) = %d, want >= 3 (summary + recent)", len(sess.History))
	}

	// First message should be the summary.
	want := "[Auto-compact summary]\n\n" + summaryText
	if sess.History[0].Content[0].Text != want {
		t.Errorf("first message = %q, want summary", sess.History[0].Content[0].Text)
	}

	// Last message should be unchanged from original.
	last := sess.History[len(sess.History)-1]
	if last.Content[0].Text != "reply3" {
		t.Errorf("last message = %q, want 'reply3'", last.Content[0].Text)
	}
}
```

**Step 2: Add test for tool-use/tool-result boundary**

```go
func TestCompact_DoesNotSplitToolPair(t *testing.T) {
	summaryText := "summary"
	events := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: summaryText},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	sess.History = []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "old"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "old reply"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "do something"}}},
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockText, Text: "calling tool"},
			{Kind: common.BlockToolUse, ToolUseID: "tc1", ToolName: "shell", InputJSON: `{"command":"ls"}`},
		}},
		{Role: common.RoleTool, Content: []common.Block{
			{Kind: common.BlockToolResult, ToolUseID: "tc1", ResultText: "file1"},
		}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "here are your files"}}},
	}

	if err := a.compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}

	// The tool_result message and its preceding assistant message should both
	// be in the kept portion — never split apart.
	for i, msg := range sess.History {
		if msg.Role == common.RoleTool && i == 0 {
			t.Error("RoleTool should not be the first message (would be split from its assistant)")
		}
		if msg.Role == common.RoleTool && i > 0 {
			prev := sess.History[i-1]
			if prev.Role != common.RoleAssistant {
				t.Errorf("message before RoleTool should be assistant, got %v", prev.Role)
			}
		}
	}
}
```

**Step 3: Add test for history too short to compact**

```go
func TestCompact_TooShortNoop(t *testing.T) {
	client := &fakeClient{}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	sess.History = []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "hello"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}},
	}

	if err := a.compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}

	// Should not have called the LLM at all.
	if client.callCount != 0 {
		t.Errorf("client called %d times, want 0 (too short to compact)", client.callCount)
	}

	// History should be unchanged.
	if len(sess.History) != 2 {
		t.Errorf("history length changed: got %d, want 2", len(sess.History))
	}
}
```

**Step 4: Run the new tests to verify they fail**

Run: `go test ./gohome/internal/agent/ -run "TestCompact_KeepsRecentMessages|TestCompact_DoesNotSplitToolPair|TestCompact_TooShortNoop" -v`

Expected: `TestCompact_KeepsRecentMessages` fails (current code replaces all history). `TestCompact_DoesNotSplitToolPair` may fail. `TestCompact_TooShortNoop` may pass or fail depending on the minimum history check.

**Step 5: Implement partial compaction in compact.go**

Replace the `compact` method in `gohome/internal/agent/compact.go` (lines 41-115):

```go
const minCompactMessages = 4

func (a *Agent) compact(ctx context.Context, sess *session.Session) error {
	if len(sess.History) < minCompactMessages {
		return nil
	}

	prompt := a.CompactPrompt
	if prompt == "" {
		prompt = defaultCompactPrompt
	}

	// Keep the last keepCount messages. Default: 4 (roughly 2 turns).
	keepCount := 4
	if keepCount >= len(sess.History) {
		return nil
	}

	splitIdx := len(sess.History) - keepCount

	// Don't split a tool-use/tool-result pair: if splitIdx lands on a
	// RoleTool message, include its preceding assistant message too.
	if splitIdx > 0 && sess.History[splitIdx].Role == common.RoleTool {
		splitIdx--
	}
	if splitIdx <= 0 {
		return nil
	}

	oldMessages := sess.History[:splitIdx]
	recentMessages := make([]common.Message, len(sess.History[splitIdx:]))
	copy(recentMessages, sess.History[splitIdx:])

	beforeTokens := 0
	for _, msg := range oldMessages {
		for _, b := range msg.Content {
			beforeTokens += len(b.Text) / 4
			beforeTokens += len(b.InputJSON) / 4
			beforeTokens += len(b.ResultText) / 4
		}
	}

	req := common.Request{
		Model:     sess.Model,
		System:    prompt,
		Messages:  oldMessages,
		MaxTokens: a.MaxTokens,
	}

	events, err := a.State.Client().Stream(ctx, req)
	if err != nil {
		return err
	}

	var sb strings.Builder
	for ev := range events {
		switch ev.Kind {
		case common.EventTextDelta:
			sb.WriteString(ev.TextDelta)
		case common.EventError:
			return ev.Err
		}
	}
	summary := sb.String()

	if summary == "" {
		slog.Warn("compact: LLM returned empty summary, skipping")
		return nil
	}

	summaryMsg := common.Message{
		Role: common.RoleUser,
		Content: []common.Block{
			{Kind: common.BlockText, Text: session.CompactSummaryPrefix + summary},
		},
	}

	sess.History = append([]common.Message{summaryMsg}, recentMessages...)

	afterTokens := len(summary) / 4

	if w := a.State.Writer(); w != nil {
		w.Emit(session.Compaction{
			BeforeTokens: beforeTokens,
			AfterTokens:  afterTokens,
			Summary:      summary,
		})
	}

	a.Frontend.Emit(sess.ID, Event{
		Kind:          EventCompacted,
		SessionID:     sess.ID,
		CompactBefore: beforeTokens,
		CompactAfter:  afterTokens,
	})

	return nil
}
```

**Step 6: Remove the old TestCompact_ReplacesHistory**

Delete `TestCompact_ReplacesHistory` from `compact_test.go` — it has been replaced by `TestCompact_KeepsRecentMessages`.

**Step 7: Update TestCompact_EmitsEvent**

This test only has 1 message in history. With the new minimum of 4 messages, it will no longer trigger compaction. Update it to have enough messages:

```go
func TestCompact_EmitsEvent(t *testing.T) {
	events := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: "summary"},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	sess.History = []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "first"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "second"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply2"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "third"}}},
	}

	if err := a.compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}

	var found bool
	for _, ev := range fe.events {
		if ev.Kind == EventCompacted {
			found = true
			if ev.CompactBefore <= 0 {
				t.Errorf("CompactBefore = %d, want > 0", ev.CompactBefore)
			}
		}
	}
	if !found {
		t.Error("no EventCompacted emitted")
	}
}
```

**Step 8: Update TestCompact_ErrorFromStream**

Same issue — needs at least 4 messages to trigger compaction:

```go
func TestCompact_ErrorFromStream(t *testing.T) {
	events := []common.StreamEvent{
		{Kind: common.EventError, Err: context.DeadlineExceeded},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	sess.History = []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "first"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "second"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply2"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "third"}}},
	}
	origLen := len(sess.History)

	err := a.compact(context.Background(), sess)
	if err == nil {
		t.Fatal("expected error from compact, got nil")
	}

	if len(sess.History) != origLen {
		t.Errorf("history length changed: got %d, want %d", len(sess.History), origLen)
	}
}
```

**Step 9: Run all compact tests**

Run: `go test ./gohome/internal/agent/ -run "TestCompact|TestShouldCompact" -v`

Expected: All pass.

**Step 10: Run full agent test suite**

Run: `go test ./gohome/internal/agent/ -v`

Expected: All pass.

**Step 11: Commit**

```bash
git add gohome/internal/agent/compact.go gohome/internal/agent/compact_test.go
git commit -m "fix(compact): use partial compaction to preserve cache-hot recent messages"
```

---

### Task 4: Fix stale E2E smoke test

**Files:**
- Modify: `gohome/test/e2e/smoke_test.go:108-116`

**Step 1: Update TestE2ESmokeRoundtrip to use SessionState**

The current test at line 108 creates an `Agent` with `Client`, `Writer`, and `Frontend` fields that no longer exist on the struct. Replace the agent construction (lines 108-116):

```go
	state := agent.NewSessionState(sess, w, client)
	a := &agent.Agent{
		Tools:    reg,
		Guard:    g,
		Frontend: fe,
		State:    state,
		System:   "You are a helpful assistant.",
		MaxTokens: 64,
	}
```

**Step 2: Extract shared E2E helper**

Add at the top of `smoke_test.go` (after the `noopFrontend` type):

```go
// e2eConfig holds the environment variables needed for E2E tests.
type e2eConfig struct {
	baseURL string
	wire    config.Wire
	model   string
	apiKey  string
}

// loadE2EConfig reads environment variables and skips if not set.
func loadE2EConfig(t *testing.T) e2eConfig {
	t.Helper()
	baseURL := os.Getenv("GOHOME_E2E_ENDPOINT")
	if baseURL == "" {
		t.Skip("GOHOME_E2E_ENDPOINT not set; skipping e2e test")
	}
	wire := config.Wire(os.Getenv("GOHOME_E2E_WIRE"))
	if wire == "" {
		wire = config.WireAnthropic
	}
	model := os.Getenv("GOHOME_E2E_MODEL")
	if model == "" {
		t.Fatal("GOHOME_E2E_MODEL must be set")
	}
	apiKey := os.Getenv("GOHOME_E2E_API_KEY")
	if apiKey == "" {
		t.Fatal("GOHOME_E2E_API_KEY must be set")
	}
	return e2eConfig{baseURL: baseURL, wire: wire, model: model, apiKey: apiKey}
}

// recordingFrontend is a noopFrontend that also records events.
type recordingFrontend struct {
	mu     sync.Mutex
	events []agent.Event
}

func (r *recordingFrontend) Emit(_ string, ev agent.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *recordingFrontend) RequestApproval(_ context.Context, _ guard.ApprovalRequest) (guard.ApprovalDecision, error) {
	return guard.ApprovalDecision{Outcome: guard.AllowOnce}, nil
}

func (r *recordingFrontend) AwaitUserInput(_ context.Context) (string, error) {
	return "", errors.New("no interactive input in e2e tests")
}

// newE2EAgent creates an Agent wired up for E2E testing.
func newE2EAgent(t *testing.T, cfg e2eConfig, fe agent.Frontend, history []common.Message) (*agent.Agent, *session.Session) {
	t.Helper()
	ep := config.ModelConfig{
		Wire:      cfg.wire,
		BaseURL:   cfg.baseURL,
		ModelName: cfg.model,
	}
	client, err := llm.New(ep, cfg.apiKey)
	if err != nil {
		t.Fatalf("llm.New: %v", err)
	}

	reg := tools.NewRegistry()

	wl, err := guard.Compile(guard.WhitelistFile{}, guard.WhitelistFile{}, "")
	if err != nil {
		t.Fatalf("guard.Compile: %v", err)
	}
	g := guard.NewGuard(wl, fe, nil)
	g.SetYolo(true)

	tmpDir := t.TempDir()
	writerPath := filepath.Join(tmpDir, "e2e.jsonl")
	w, err := session.OpenWriter(writerPath)
	if err != nil {
		t.Fatalf("session.OpenWriter: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	sess := session.NewSession("e2e-test", tmpDir, cfg.model, string(cfg.wire))
	sess.History = history

	state := agent.NewSessionState(sess, w, client)
	a := &agent.Agent{
		Tools:    reg,
		Guard:    g,
		Frontend: fe,
		State:    state,
		System:   "You are a helpful assistant.",
		MaxTokens: 256,
	}

	return a, sess
}
```

Add `"sync"` to the import block.

**Step 3: Simplify TestE2ESmokeRoundtrip to use the helper**

```go
func TestE2ESmokeRoundtrip(t *testing.T) {
	cfg := loadE2EConfig(t)
	fe := &recordingFrontend{}
	history := []common.Message{
		{
			Role: common.RoleUser,
			Content: []common.Block{
				{Kind: common.BlockText, Text: "Reply with the single word: pong."},
			},
		},
	}

	a, sess := newE2EAgent(t, cfg, fe, history)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := a.Run(ctx, sess); err != nil {
		t.Fatalf("agent.Run: %v", err)
	}

	var lastAssistantText string
	for _, msg := range sess.History {
		if msg.Role == common.RoleAssistant {
			for _, b := range msg.Content {
				if b.Kind == common.BlockText {
					lastAssistantText = b.Text
				}
			}
		}
	}
	if lastAssistantText == "" {
		t.Error("last assistant message text is empty")
	}
	t.Logf("assistant replied: %q", lastAssistantText)
}
```

**Step 4: Verify compilation**

Run: `go build -tags e2e ./gohome/test/e2e/`

Expected: Compiles without errors.

**Step 5: Commit**

```bash
git add gohome/test/e2e/smoke_test.go
git commit -m "fix(e2e): update smoke test to use SessionState, extract shared helper"
```

---

### Task 5: Add E2E test for headless tool calls

**Files:**
- Modify: `gohome/test/e2e/smoke_test.go`

**Step 1: Register the shell tool in newE2EAgent**

The `newE2EAgent` helper creates an empty registry. To test tool calls, we need tools registered. Modify `newE2EAgent` to register the tools that exist in the project. Import the shell/read tools or register them from the existing tool registry.

Check what tools are available and how they're registered. The simplest approach: use the real `tools.RegisterDefaults` if it exists, or manually register the `read` tool.

First, check: `grep -rn "RegisterDefaults\|RegisterAll\|Register(" gohome/internal/tools/registry.go`

If there's no bulk-register function, register the read tool directly in the helper:

```go
import "github.com/jhyoong/GoHome/gohome/internal/tools"
```

In `newE2EAgent`, after `reg := tools.NewRegistry()`:

```go
	tools.RegisterDefaults(reg)
```

Or if that function doesn't exist, register only the read tool:

```go
	reg.Register(tools.NewReadTool())
```

Verify the exact function names and constructor signatures by reading the tools package before implementing.

**Step 2: Write TestE2EHeadlessToolCall**

```go
func TestE2EHeadlessToolCall(t *testing.T) {
	cfg := loadE2EConfig(t)
	fe := &recordingFrontend{}

	// Create a temp file with known content.
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello-gohome"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	prompt := fmt.Sprintf("Read the file at %s and tell me its exact content. Reply with just the content, nothing else.", testFile)
	history := []common.Message{
		{
			Role:    common.RoleUser,
			Content: []common.Block{{Kind: common.BlockText, Text: prompt}},
		},
	}

	a, sess := newE2EAgent(t, cfg, fe, history)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	if err := a.Run(ctx, sess); err != nil {
		t.Fatalf("agent.Run: %v", err)
	}

	// Verify a tool was called.
	fe.mu.Lock()
	var sawToolResult bool
	for _, ev := range fe.events {
		if ev.Kind == agent.EventToolResult {
			sawToolResult = true
		}
	}
	fe.mu.Unlock()
	if !sawToolResult {
		t.Error("expected at least one EventToolResult (tool call)")
	}

	// Verify the assistant response contains the file content.
	var lastAssistantText string
	for _, msg := range sess.History {
		if msg.Role == common.RoleAssistant {
			for _, b := range msg.Content {
				if b.Kind == common.BlockText {
					lastAssistantText = b.Text
				}
			}
		}
	}
	if !strings.Contains(lastAssistantText, "hello-gohome") {
		t.Errorf("assistant reply should contain 'hello-gohome', got: %q", lastAssistantText)
	}
	t.Logf("assistant replied: %q", lastAssistantText)
}
```

Add `"fmt"` and `"strings"` to the import block if not already present.

**Step 3: Verify compilation**

Run: `go build -tags e2e ./gohome/test/e2e/`

Expected: Compiles without errors.

**Step 4: Commit**

```bash
git add gohome/test/e2e/smoke_test.go
git commit -m "test(e2e): add headless tool call test with real LLM endpoint"
```

---

### Task 6: Add E2E test for multi-turn interactive headless mode

**Files:**
- Modify: `gohome/test/e2e/smoke_test.go`

**Step 1: Write TestE2EHeadlessMultiTurn**

```go
func TestE2EHeadlessMultiTurn(t *testing.T) {
	cfg := loadE2EConfig(t)

	// Prepare JSONL input: two user messages then exit.
	input := strings.Join([]string{
		`{"type":"user_message","content":"What is 2+2? Reply with just the number."}`,
		`{"type":"exit"}`,
	}, "\n") + "\n"

	var output strings.Builder
	hfe := headless.NewInteractiveFrontend(
		strings.NewReader(input),
		true,
		&output,
	)

	ep := config.ModelConfig{
		Wire:      cfg.wire,
		BaseURL:   cfg.baseURL,
		ModelName: cfg.model,
	}
	client, err := llm.New(ep, cfg.apiKey)
	if err != nil {
		t.Fatalf("llm.New: %v", err)
	}

	reg := tools.NewRegistry()

	wl, werr := guard.Compile(guard.WhitelistFile{}, guard.WhitelistFile{}, "")
	if werr != nil {
		t.Fatalf("guard.Compile: %v", werr)
	}
	g := guard.NewGuard(wl, hfe, nil)
	g.SetYolo(true)

	tmpDir := t.TempDir()
	writerPath := filepath.Join(tmpDir, "e2e-multi.jsonl")
	w, err := session.OpenWriter(writerPath)
	if err != nil {
		t.Fatalf("session.OpenWriter: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	sess := session.NewSession("e2e-multi", tmpDir, cfg.model, string(cfg.wire))

	state := agent.NewSessionState(sess, w, client)
	a := &agent.Agent{
		Tools:    reg,
		Guard:    g,
		Frontend: hfe,
		State:    state,
		System:   "You are a helpful assistant.",
		MaxTokens: 64,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Run the interactive loop: read input, run agent, repeat until exit.
	for {
		userText, inputErr := hfe.AwaitUserInput(ctx)
		if inputErr != nil {
			if errors.Is(inputErr, headless.ErrExit) {
				break
			}
			t.Fatalf("AwaitUserInput: %v", inputErr)
		}

		sess.History = append(sess.History, common.Message{
			Role:    common.RoleUser,
			Content: []common.Block{{Kind: common.BlockText, Text: userText}},
		})

		if runErr := a.Run(ctx, sess); runErr != nil {
			t.Fatalf("agent.Run: %v", runErr)
		}
	}

	// Verify the agent responded with something containing "4".
	var lastAssistantText string
	for _, msg := range sess.History {
		if msg.Role == common.RoleAssistant {
			for _, b := range msg.Content {
				if b.Kind == common.BlockText {
					lastAssistantText = b.Text
				}
			}
		}
	}
	if !strings.Contains(lastAssistantText, "4") {
		t.Errorf("assistant should have responded with '4', got: %q", lastAssistantText)
	}

	// Verify verbose output was written (JSONL events on stdout).
	if output.Len() == 0 {
		t.Error("expected verbose JSONL output, got nothing")
	}
	t.Logf("assistant replied: %q", lastAssistantText)
	t.Logf("output length: %d bytes", output.Len())
}
```

Add the `headless` import: `"github.com/jhyoong/GoHome/gohome/internal/headless"`

**Step 2: Verify compilation**

Run: `go build -tags e2e ./gohome/test/e2e/`

Expected: Compiles without errors.

**Step 3: Commit**

```bash
git add gohome/test/e2e/smoke_test.go
git commit -m "test(e2e): add multi-turn interactive headless test with real LLM"
```

---

### Task 7: Add E2E test for auto compaction

**Files:**
- Modify: `gohome/test/e2e/smoke_test.go`

**Step 1: Write TestE2EAutoCompact**

```go
func TestE2EAutoCompact(t *testing.T) {
	cfg := loadE2EConfig(t)
	fe := &recordingFrontend{}

	// Seed with a long conversation history to trigger compaction.
	var history []common.Message
	for i := 0; i < 10; i++ {
		history = append(history,
			common.Message{
				Role:    common.RoleUser,
				Content: []common.Block{{Kind: common.BlockText, Text: fmt.Sprintf("Tell me about topic %d in detail.", i)}},
			},
			common.Message{
				Role:    common.RoleAssistant,
				Content: []common.Block{{Kind: common.BlockText, Text: strings.Repeat(fmt.Sprintf("Here is a detailed explanation about topic %d. ", i), 50)}},
			},
		)
	}
	// Final prompt.
	history = append(history, common.Message{
		Role:    common.RoleUser,
		Content: []common.Block{{Kind: common.BlockText, Text: "Summarize everything we discussed. Reply briefly."}},
	})

	a, sess := newE2EAgent(t, cfg, fe, history)

	// Enable auto-compact with a very low trigger threshold.
	a.CompactCfg = agent.CompactConfig{
		Enabled:       true,
		Mode:          "percentage",
		TriggerPct:    0.01, // trigger almost immediately
		ContextWindow: 128000,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	if err := a.Run(ctx, sess); err != nil {
		t.Fatalf("agent.Run: %v", err)
	}

	// Verify compaction fired.
	fe.mu.Lock()
	var sawCompacted bool
	for _, ev := range fe.events {
		if ev.Kind == agent.EventCompacted {
			sawCompacted = true
			t.Logf("compaction: %d -> %d tokens", ev.CompactBefore, ev.CompactAfter)
		}
	}
	fe.mu.Unlock()
	if !sawCompacted {
		t.Error("expected EventCompacted but it was not emitted")
	}

	// Verify the agent still produced a response after compaction.
	var lastAssistantText string
	for _, msg := range sess.History {
		if msg.Role == common.RoleAssistant {
			for _, b := range msg.Content {
				if b.Kind == common.BlockText {
					lastAssistantText = b.Text
				}
			}
		}
	}
	if lastAssistantText == "" {
		t.Error("no assistant response after compaction")
	}
	t.Logf("post-compaction reply length: %d chars", len(lastAssistantText))
}
```

**Step 2: Verify compilation**

Run: `go build -tags e2e ./gohome/test/e2e/`

Expected: Compiles without errors.

**Step 3: Commit**

```bash
git add gohome/test/e2e/smoke_test.go
git commit -m "test(e2e): add auto-compaction test with real LLM endpoint"
```

---

### Task 8: Write Windows Defender documentation

**Files:**
- Create: `docs/windows-defender.md`
- Modify: `README.md`

**Step 1: Create docs/windows-defender.md**

```markdown
# Windows Defender False Positive

Windows Defender (and some other antivirus engines) may flag the gohome binary as a trojan or potentially unwanted program. This is a false positive.

## Why it happens

gohome is a terminal coding agent. Its core features — executing shell commands, writing files, and communicating with remote LLM APIs — overlap with behavioral patterns that antivirus heuristics associate with malware. The combination of several of these patterns in a single binary pushes the heuristic score above detection thresholds.

## Specific triggers

| Behavior | Why AV flags it | Why it is a false positive |
|---|---|---|
| Invokes `powershell.exe -NoProfile -NonInteractive` with arbitrary commands | Matches RAT/backdoor command execution patterns | Core feature: executes commands requested by the user or LLM |
| Writes arbitrary content to arbitrary file paths | Matches dropper/payload-writing behavior | Core feature: the file write and edit tools for code editing |
| Makes outbound HTTPS requests to configurable remote endpoints | Matches command-and-control (C2) communication | Connects to LLM API endpoints (Anthropic, OpenAI-compatible) |
| Binary is stripped of debug symbols (`-s -w` linker flags) | Common in malware to hinder reverse engineering | Standard Go release build optimization to reduce binary size |
| Binary is unsigned (no Authenticode certificate) | Unknown publisher, higher threat score from SmartScreen | Open-source project, code signing not yet set up |
| Reads API keys from environment variables and sends them as HTTP headers | Matches credential harvesting behavior | Standard API authentication for LLM endpoints |
| Can run headlessly with no user interaction (`--yolo --prompt`) | Matches automated payload delivery | Designed for scripted/CI usage of the coding agent |
| Spawns sub-processes (subagents) that independently execute commands | Matches multi-stage dropper behavior | Subagent feature for parallel coding tasks |
| Accesses the clipboard | Common data exfiltration vector | Copy/paste support in the TUI |

## Workaround

Add a Windows Defender exclusion for the gohome binary:

### Via Windows Security settings

1. Open **Windows Security** (search "Windows Security" in the Start menu).
2. Go to **Virus & threat protection** > **Virus & threat protection settings** > **Manage settings**.
3. Scroll to **Exclusions** > **Add or remove exclusions**.
4. Click **Add an exclusion** > **File**, then select `gohome.exe`.

### Via PowerShell (administrator)

```powershell
Add-MpPreference -ExclusionPath "C:\path\to\gohome.exe"
```

Replace the path with the actual location of your gohome binary.

## Future mitigations

These are planned improvements that may reduce or eliminate false positives:

- **Authenticode code signing** with an EV certificate to establish publisher trust.
- **Submitting the binary to Microsoft** via their [false positive reporting portal](https://www.microsoft.com/en-us/wdsi/filesubmission) for whitelisting.
- **Building natively on Windows** instead of cross-compiling from Linux, which can produce binaries with different internal structures that trigger entropy-based scanners.
- **Removing `-s -w` strip flags** from Windows release builds to preserve debug symbols.
```

**Step 2: Add troubleshooting section to README.md**

At the end of `README.md` (before the final blank line at line 286), add:

```markdown
## Troubleshooting

### Windows Defender False Positive

Windows Defender may flag gohome as a trojan. This is a false positive caused by the binary's legitimate coding agent features (shell execution, file writes, API communication) overlapping with malware behavioral patterns. See [docs/windows-defender.md](docs/windows-defender.md) for details and workaround instructions.
```

**Step 3: Commit**

```bash
git add docs/windows-defender.md README.md
git commit -m "docs: add Windows Defender false positive explanation and workaround"
```

---

### Task 9: Run full test suite and lint

**Step 1: Run all unit tests**

Run: `go test ./gohome/...`

Expected: All pass.

**Step 2: Run vet**

Run: `go vet ./gohome/...`

Expected: No issues.

**Step 3: Run lint**

Run: `golangci-lint run ./gohome/...`

Expected: No new issues.

**Step 4: Verify E2E tests compile**

Run: `go build -tags e2e ./gohome/test/e2e/`

Expected: Compiles without errors.
