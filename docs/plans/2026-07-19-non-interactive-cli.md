# Non-Interactive CLI Mode Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `--prompt` flag that runs gohome headlessly — single prompt in, final text out, no TUI.

**Architecture:** New `internal/headless` package implements `agent.Frontend`. `main.go` branches after shared setup: headless path calls `a.Run()` directly, TUI path unchanged.

**Tech Stack:** Go standard library, existing `agent.Frontend` interface, `encoding/json` for JSONL verbose output.

---

### Task 1: Create headless Frontend struct and constructor

**Files:**
- Create: `gohome/internal/headless/frontend.go`

**Step 1: Write the failing test**

Create `gohome/internal/headless/frontend_test.go`:

```go
package headless

import (
	"bytes"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
)

// Compile-time check: Frontend implements agent.Frontend.
var _ agent.Frontend = (*Frontend)(nil)

func TestNewFrontend(t *testing.T) {
	var buf bytes.Buffer
	fe := NewFrontend("hello", false, &buf)
	if fe == nil {
		t.Fatal("NewFrontend returned nil")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/headless/ -run TestNewFrontend -v`
Expected: FAIL — package does not exist.

**Step 3: Write minimal implementation**

Create `gohome/internal/headless/frontend.go`:

```go
package headless

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
)

type Frontend struct {
	prompt  string
	verbose bool
	output  io.Writer
	sent    bool
	buf     strings.Builder
	mu      sync.Mutex
}

func NewFrontend(prompt string, verbose bool, output io.Writer) *Frontend {
	return &Frontend{
		prompt:  prompt,
		verbose: verbose,
		output:  output,
	}
}

func (f *Frontend) AwaitUserInput(ctx context.Context) (string, error) {
	f.mu.Lock()
	if !f.sent {
		f.sent = true
		f.mu.Unlock()
		return f.prompt, nil
	}
	f.mu.Unlock()
	<-ctx.Done()
	return "", ctx.Err()
}

func (f *Frontend) RequestApproval(_ context.Context, _ guard.ApprovalRequest) (guard.ApprovalDecision, error) {
	return guard.ApprovalDecision{Outcome: guard.AllowOnce}, nil
}

func (f *Frontend) Emit(_ string, ev agent.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.verbose {
		data, err := json.Marshal(ev)
		if err == nil {
			f.output.Write(data)
			f.output.Write([]byte("\n"))
		}
		return
	}

	if ev.Kind == agent.EventTokenDelta {
		f.buf.WriteString(ev.TextDelta)
	}
}

func (f *Frontend) FinalText() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.buf.String()
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/headless/ -run TestNewFrontend -v`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/headless/
git commit -m "feat(headless): add headless Frontend implementing agent.Frontend"
```

---

### Task 2: Unit tests for AwaitUserInput

**Files:**
- Modify: `gohome/internal/headless/frontend_test.go`

**Step 1: Write tests**

Append to `frontend_test.go`:

```go
func TestAwaitUserInput_FirstCall(t *testing.T) {
	var buf bytes.Buffer
	fe := NewFrontend("do something", false, &buf)

	ctx := context.Background()
	text, err := fe.AwaitUserInput(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "do something" {
		t.Errorf("got %q, want %q", text, "do something")
	}
}

func TestAwaitUserInput_SecondCall_Blocks(t *testing.T) {
	var buf bytes.Buffer
	fe := NewFrontend("hello", false, &buf)

	ctx := context.Background()
	_, _ = fe.AwaitUserInput(ctx) // consume first call

	ctx2, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := fe.AwaitUserInput(ctx2)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
```

**Step 2: Run tests**

Run: `go test ./gohome/internal/headless/ -run TestAwaitUserInput -v`
Expected: PASS (implementation already handles this).

**Step 3: Commit**

```bash
git add gohome/internal/headless/frontend_test.go
git commit -m "test(headless): add AwaitUserInput unit tests"
```

---

### Task 3: Unit tests for Emit (plain text and verbose)

**Files:**
- Modify: `gohome/internal/headless/frontend_test.go`

**Step 1: Write tests**

Append to `frontend_test.go`:

```go
func TestEmit_PlainText_AccumulatesTokenDeltas(t *testing.T) {
	var buf bytes.Buffer
	fe := NewFrontend("x", false, &buf)

	fe.Emit("s1", agent.Event{Kind: agent.EventTokenDelta, TextDelta: "Hello"})
	fe.Emit("s1", agent.Event{Kind: agent.EventTokenDelta, TextDelta: " world"})
	fe.Emit("s1", agent.Event{Kind: agent.EventSending})   // ignored
	fe.Emit("s1", agent.Event{Kind: agent.EventTurnDone})  // ignored

	got := fe.FinalText()
	if got != "Hello world" {
		t.Errorf("FinalText() = %q, want %q", got, "Hello world")
	}

	// stdout should be empty in plain mode
	if buf.Len() != 0 {
		t.Errorf("expected no output to writer in plain mode, got %q", buf.String())
	}
}

func TestEmit_Verbose_WritesJSONLines(t *testing.T) {
	var buf bytes.Buffer
	fe := NewFrontend("x", true, &buf)

	fe.Emit("s1", agent.Event{Kind: agent.EventTokenDelta, TextDelta: "hi"})
	fe.Emit("s1", agent.Event{Kind: agent.EventTurnDone, StopReason: "end_turn"})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}

	// Verify first line is valid JSON with expected kind
	var ev1 map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &ev1); err != nil {
		t.Fatalf("line 1 not valid JSON: %v", err)
	}
	if ev1["kind"] != "token_delta" {
		t.Errorf("line 1 kind = %v, want token_delta", ev1["kind"])
	}
}
```

**Step 2: Run tests**

Run: `go test ./gohome/internal/headless/ -run TestEmit -v`
Expected: PASS

**Step 3: Commit**

```bash
git add gohome/internal/headless/frontend_test.go
git commit -m "test(headless): add Emit unit tests for plain and verbose modes"
```

---

### Task 4: Unit test for RequestApproval

**Files:**
- Modify: `gohome/internal/headless/frontend_test.go`

**Step 1: Write test**

Append to `frontend_test.go`:

```go
func TestRequestApproval_ReturnsAllowOnce(t *testing.T) {
	var buf bytes.Buffer
	fe := NewFrontend("x", false, &buf)

	dec, err := fe.RequestApproval(context.Background(), guard.ApprovalRequest{
		Tool:    "shell",
		Summary: "rm -rf /",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Outcome != guard.AllowOnce {
		t.Errorf("outcome = %v, want AllowOnce", dec.Outcome)
	}
}
```

**Step 2: Run test**

Run: `go test ./gohome/internal/headless/ -run TestRequestApproval -v`
Expected: PASS

**Step 3: Commit**

```bash
git add gohome/internal/headless/frontend_test.go
git commit -m "test(headless): add RequestApproval unit test"
```

---

### Task 5: Add --prompt and --verbose flags to main.go

**Files:**
- Modify: `gohome/cmd/gohome/main.go:32-38` (flag declarations)

**Step 1: Add flag declarations**

Add to the `var` block at line 32:

```go
var (
	modelFlag   = flag.String("model", "", "model config name override")
	yolo        = flag.Bool("yolo", false, "disable all approval prompts")
	resume      = flag.Bool("resume", false, "resume a past session")
	showVersion = flag.Bool("version", false, "print version and exit")
	showConfig  = flag.Bool("config", false, "print merged configuration and exit")
	prompt      = flag.String("prompt", "", "run non-interactively with this prompt (requires --yolo)")
	verbose     = flag.Bool("verbose", false, "emit all events as JSON lines (requires --prompt)")
)
```

**Step 2: Add validation after the `--config` check (around line 203)**

Insert after the `showConfig` block:

```go
if *prompt != "" && !*yolo {
	fmt.Fprintf(os.Stderr, "gohome: --prompt (non-interactive mode) requires --yolo\n")
	os.Exit(1)
}
if *verbose && *prompt == "" {
	fmt.Fprintf(os.Stderr, "gohome: --verbose requires --prompt\n")
	os.Exit(1)
}
if *prompt == "" && flag.NArg() == 0 {
	// Normal interactive mode, no validation needed.
} else if *prompt == "" {
	// Positional args without --prompt: ignore (normal mode).
}
```

**Step 3: Verify it compiles**

Run: `go build -o /dev/null ./gohome/cmd/gohome`
Expected: compiles without error.

**Step 4: Commit**

```bash
git add gohome/cmd/gohome/main.go
git commit -m "feat: add --prompt and --verbose flag declarations with validation"
```

---

### Task 6: Implement headless execution path in main.go

**Files:**
- Modify: `gohome/cmd/gohome/main.go`

**Step 1: Add import for headless package**

Add to imports:

```go
"github.com/jhyoong/GoHome/gohome/internal/headless"
```

**Step 2: Branch into headless path**

Replace the block starting at "Build frontend and guard" (line 264) through the end of main with the branched logic. The key change: if `*prompt != ""`, skip TUI setup entirely and run the headless path.

After the session writer setup and session_start emit (line 326), insert the headless branch:

```go
if *prompt != "" {
	// Headless path: non-interactive execution.
	fe := headless.NewFrontend(*prompt, *verbose, os.Stdout)
	g := guard.NewGuard(wl, fe)
	g.SetYolo(*yolo)

	// Build tools registry.
	registry := tools.NewRegistry()
	registry.Register(tools.ReadTool{})
	registry.Register(tools.WriteTool{})
	registry.Register(tools.EditTool{})
	registry.Register(tools.ShellTool{
		DefaultTimeoutMs: settings.ShellTimeoutMs,
		MaxTimeoutMs:     settings.MaxShellTimeoutMs,
	})

	// System prompt.
	systemPrompt := `You are gohome, an AI coding assistant. You help users with software development tasks.
You have access to tools for reading and writing files, running shell commands, and spawning subagents for parallel work.
Be concise and precise. Ask for clarification when requirements are ambiguous.`
	if settings.SystemPrompt != "" {
		systemPrompt = settings.SystemPrompt
	}

	maxTokens := mc.MaxTokens
	if maxTokens <= 0 {
		maxTokens = config.DefaultMaxTokens
	}
	thinkingBudget := mc.ThinkingBudget
	if thinkingBudget <= 0 {
		thinkingBudget = config.DefaultThinkingBudget
	}

	contextWindow := mc.ContextWindow
	if contextWindow <= 0 {
		contextWindow = config.DefaultContextWindow
	}
	compactCfg := agent.CompactConfig{
		Enabled:       settings.AutoCompact,
		Mode:          settings.AutoCompactMode,
		TriggerPct:    settings.AutoCompactPct,
		TargetPct:     settings.AutoCompactTargetPct,
		Leftover:      settings.AutoCompactLeftover,
		ContextWindow: contextWindow,
	}
	if compactCfg.Mode == "" {
		compactCfg.Mode = "percentage"
	}
	if compactCfg.TriggerPct <= 0 {
		compactCfg.TriggerPct = config.DefaultAutoCompactPct
	}
	if compactCfg.TargetPct <= 0 {
		compactCfg.TargetPct = config.DefaultAutoCompactTargetPct
	}
	if compactCfg.Leftover <= 0 {
		compactCfg.Leftover = config.DefaultAutoCompactLeftover
	}

	state := agent.NewSessionState(sess, writer, client)
	a := &agent.Agent{
		Tools:           registry,
		Guard:           g,
		Frontend:        fe,
		State:           state,
		System:          systemPrompt,
		MaxTokens:       maxTokens,
		ThinkingBudget:  thinkingBudget,
		ReasoningEffort: mc.ReasoningEffort,
		CompactCfg:      compactCfg,
		CompactPrompt:   settings.AutoCompactPrompt,
		Home:            home,
	}
	a.RegisterSubagentTool()

	// Inject prompt as user message.
	sess.History = append(sess.History, common.Message{
		Role: common.RoleUser,
		Content: []common.Block{
			{Kind: common.BlockText, Text: *prompt},
		},
	})
	writer.Emit(session.UserMessage{
		Content: []common.Block{
			{Kind: common.BlockText, Text: *prompt},
		},
	})

	// Run agent.
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	runErr := a.Run(ctx, sess)
	signal.Stop(sigCh)
	cancel()

	// Output final text (plain mode only).
	if !*verbose {
		text := fe.FinalText()
		if text != "" {
			fmt.Print(text)
			if !strings.HasSuffix(text, "\n") {
				fmt.Println()
			}
		}
	}

	// Shutdown.
	writer.Emit(session.SessionEnd{Reason: "prompt_done"})
	_ = writer.Close()
	if logFile != nil {
		_ = logFile.Close()
	}

	if runErr != nil && runErr != context.Canceled {
		fmt.Fprintf(os.Stderr, "gohome: agent error: %v\n", runErr)
		os.Exit(1)
	}
	return
}
```

The existing TUI path (everything after this block) remains unchanged inside an implicit else (the `return` ends the headless path).

**Step 3: Verify it compiles**

Run: `go build -o /dev/null ./gohome/cmd/gohome`
Expected: compiles without error.

**Step 4: Commit**

```bash
git add gohome/cmd/gohome/main.go
git commit -m "feat: implement headless execution path for --prompt mode"
```

---

### Task 7: Integration test with fake LLM client

**Files:**
- Create: `gohome/internal/headless/integration_test.go`

**Step 1: Write integration test**

```go
package headless_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/headless"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

type fakeClient struct {
	response string
}

func (c *fakeClient) Stream(_ context.Context, msgs []common.Message, _ common.RequestParams) (<-chan common.StreamEvent, error) {
	ch := make(chan common.StreamEvent, 3)
	ch <- common.StreamEvent{Type: common.EventContentDelta, Text: c.response}
	ch <- common.StreamEvent{Type: common.EventUsage, Usage: &common.Usage{InputTokens: 10, OutputTokens: 5}}
	ch <- common.StreamEvent{Type: common.EventDone, StopReason: "end_turn"}
	close(ch)
	return ch, nil
}

func TestHeadless_EndToEnd(t *testing.T) {
	var buf bytes.Buffer
	fe := headless.NewFrontend("say hello", false, &buf)

	client := &fakeClient{response: "Hello!"}
	sess := session.NewSession("test-id", t.TempDir(), "fake-model", "fake")
	writerPath := t.TempDir() + "/test.jsonl"
	writer, err := session.OpenWriter(writerPath)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	wl, _ := guard.LoadWhitelist("", "")
	g := guard.NewGuard(wl, fe)
	g.SetYolo(true)

	registry := tools.NewRegistry()
	state := agent.NewSessionState(sess, writer, client)

	a := &agent.Agent{
		Tools:    registry,
		Guard:    g,
		Frontend: fe,
		State:    state,
		System:   "You are a test agent.",
	}

	sess.History = append(sess.History, common.Message{
		Role:    common.RoleUser,
		Content: []common.Block{{Kind: common.BlockText, Text: "say hello"}},
	})

	err = a.Run(context.Background(), sess)
	if err != nil {
		t.Fatalf("agent.Run failed: %v", err)
	}

	got := fe.FinalText()
	if got != "Hello!" {
		t.Errorf("FinalText() = %q, want %q", got, "Hello!")
	}
}
```

**Step 2: Run test**

Run: `go test ./gohome/internal/headless/ -run TestHeadless_EndToEnd -v`
Expected: PASS

Note: The `fakeClient` struct must match the `common.Client` interface exactly. Check the interface signature in `gohome/internal/llm/common/` and adjust if needed (method name might be `Chat` or `Stream`).

**Step 3: Commit**

```bash
git add gohome/internal/headless/integration_test.go
git commit -m "test(headless): add end-to-end integration test with fake LLM"
```

---

### Task 8: Verify with go vet and golangci-lint

**Step 1: Run vet**

Run: `go vet ./gohome/...`
Expected: no errors.

**Step 2: Run lint**

Run: `golangci-lint run ./gohome/...`
Expected: no new warnings from headless package or main.go changes.

**Step 3: Run full test suite**

Run: `go test ./gohome/...`
Expected: all tests pass, including existing TUI and agent tests.

**Step 4: Build binary and smoke test**

Run:
```bash
go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome

# Should fail with missing --yolo error:
./bin/gohome --prompt "hello" 2>&1 | grep "requires --yolo"

# Should fail with --verbose without --prompt:
./bin/gohome --verbose 2>&1 | grep "requires --prompt"
```

**Step 5: Commit any fixups from vet/lint**

```bash
git add -A && git commit -m "fix: address vet/lint findings in headless implementation"
```

(Skip this commit if no fixes were needed.)

---

### Task 9: Manual smoke test with live LLM

This task requires a configured LLM endpoint. Skip if not available.

**Step 1: Basic prompt**

```bash
./bin/gohome --prompt "What is 2+2? Answer with just the number." --yolo
```

Expected: prints `4` (or similar short response) to stdout and exits 0.

**Step 2: Tool-using prompt**

```bash
./bin/gohome --prompt "Read the file go.mod and tell me the Go version." --yolo
```

Expected: prints something like `The Go version is 1.25.` and exits 0.

**Step 3: Verbose mode**

```bash
./bin/gohome --prompt "What is 2+2?" --yolo --verbose | head -5
```

Expected: JSON lines with `token_delta` events visible.

**Step 4: Session persisted**

```bash
ls ~/.gohome/sessions/
```

Expected: a new JSONL file exists for the headless session.
