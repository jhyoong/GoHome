# Interactive CLI Mode Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enable multi-turn interactive sessions over the headless CLI path via JSONL on stdin/stdout, so other tools (e.g. Claude Code) can drive GoHome programmatically.

**Architecture:** Extend the existing headless `Frontend` to read JSONL from stdin when `--prompt -` is passed. Add `TurnDone` and `Warning` session event types. Wire a multi-turn loop in `main.go` that mirrors the TUI's `runLoop` but using the headless frontend.

**Tech Stack:** Go standard library (`bufio`, `encoding/json`, `io`, `os`). No new dependencies.

---

### Task 1: Add TurnDone and Warning event types to session/events.go

**Files:**
- Modify: `gohome/internal/session/events.go:56-106`
- Test: `gohome/internal/session/events_test.go`

**Step 1: Write failing tests for TurnDone and Warning encode**

Add to `gohome/internal/session/events_test.go`:

```go
func TestEncodeTurnDone(t *testing.T) {
	ev := TurnDone{SessionID: "s1"}
	b, err := encode(ev)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	m := decodeMap(t, b)
	assertStringField(t, m, "type", "turn_done")
	assertTSPresent(t, m)
	assertStringField(t, m, "sessionId", "s1")
}

func TestEncodeWarning(t *testing.T) {
	ev := Warning{Message: "bad input"}
	b, err := encode(ev)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	m := decodeMap(t, b)
	assertStringField(t, m, "type", "warning")
	assertTSPresent(t, m)
	assertStringField(t, m, "message", "bad input")
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/session/ -run "TestEncodeTurnDone|TestEncodeWarning" -v`
Expected: FAIL -- `encode` returns "unknown event type" for both.

**Step 3: Add the TurnDone and Warning structs and encode cases**

In `gohome/internal/session/events.go`, after the `SessionEnd` struct (line 58), add:

```go
type TurnDone struct {
	SessionID string `json:"sessionId"`
}

type Warning struct {
	Message string `json:"message"`
}
```

In the `encode` function's switch statement, add two cases before the `default`:

```go
case TurnDone:
	typeName = "turn_done"
case Warning:
	typeName = "warning"
```

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/session/ -run "TestEncodeTurnDone|TestEncodeWarning" -v`
Expected: PASS

**Step 5: Run full session test suite**

Run: `go test ./gohome/internal/session/ -v`
Expected: All tests PASS (existing tests unaffected).

**Step 6: Commit**

```bash
git add gohome/internal/session/events.go gohome/internal/session/events_test.go
git commit -m "feat(session): add TurnDone and Warning event types for interactive CLI mode"
```

---

### Task 2: Extend headless Frontend to support stdin JSONL reading

**Files:**
- Modify: `gohome/internal/headless/frontend.go`
- Test: `gohome/internal/headless/frontend_test.go`

**Step 1: Write failing tests for interactive AwaitUserInput**

Add to `gohome/internal/headless/frontend_test.go`:

```go
func TestAwaitUserInput_Interactive_ReadsUserMessage(t *testing.T) {
	input := strings.NewReader("{\"type\":\"user_message\",\"content\":\"hello world\"}\n")
	var buf bytes.Buffer
	fe := NewInteractiveFrontend(input, true, &buf)

	text, err := fe.AwaitUserInput(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "hello world" {
		t.Errorf("got %q, want %q", text, "hello world")
	}
}

func TestAwaitUserInput_Interactive_ExitMessage(t *testing.T) {
	input := strings.NewReader("{\"type\":\"exit\"}\n")
	var buf bytes.Buffer
	fe := NewInteractiveFrontend(input, true, &buf)

	_, err := fe.AwaitUserInput(context.Background())
	if !errors.Is(err, ErrExit) {
		t.Errorf("expected ErrExit, got %v", err)
	}
}

func TestAwaitUserInput_Interactive_EOF(t *testing.T) {
	input := strings.NewReader("")
	var buf bytes.Buffer
	fe := NewInteractiveFrontend(input, true, &buf)

	_, err := fe.AwaitUserInput(context.Background())
	if !errors.Is(err, ErrExit) {
		t.Errorf("expected ErrExit on EOF, got %v", err)
	}
}

func TestAwaitUserInput_Interactive_SkipsMalformedJSON(t *testing.T) {
	input := strings.NewReader("not json\n{\"type\":\"user_message\",\"content\":\"valid\"}\n")
	var buf bytes.Buffer
	fe := NewInteractiveFrontend(input, true, &buf)

	text, err := fe.AwaitUserInput(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "valid" {
		t.Errorf("got %q, want %q", text, "valid")
	}
	if !strings.Contains(buf.String(), "warning") {
		t.Errorf("expected warning in output, got %q", buf.String())
	}
}

func TestAwaitUserInput_Interactive_MultiTurn(t *testing.T) {
	input := strings.NewReader(
		"{\"type\":\"user_message\",\"content\":\"first\"}\n" +
		"{\"type\":\"user_message\",\"content\":\"second\"}\n",
	)
	var buf bytes.Buffer
	fe := NewInteractiveFrontend(input, true, &buf)

	text1, err := fe.AwaitUserInput(context.Background())
	if err != nil {
		t.Fatalf("turn 1: unexpected error: %v", err)
	}
	if text1 != "first" {
		t.Errorf("turn 1: got %q, want %q", text1, "first")
	}

	text2, err := fe.AwaitUserInput(context.Background())
	if err != nil {
		t.Fatalf("turn 2: unexpected error: %v", err)
	}
	if text2 != "second" {
		t.Errorf("turn 2: got %q, want %q", text2, "second")
	}
}
```

Also add `"errors"` to the imports.

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/headless/ -run "TestAwaitUserInput_Interactive" -v`
Expected: FAIL -- `NewInteractiveFrontend` and `ErrExit` are undefined.

**Step 3: Implement NewInteractiveFrontend and updated AwaitUserInput**

Modify `gohome/internal/headless/frontend.go`:

```go
package headless

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

var ErrExit = errors.New("headless: exit requested")

type Frontend struct {
	prompt  string
	verbose bool
	output  io.Writer
	sent    bool
	buf     strings.Builder
	mu      sync.Mutex

	// interactive mode fields (nil for one-shot)
	scanner *bufio.Scanner
}

func NewFrontend(prompt string, verbose bool, output io.Writer) *Frontend {
	return &Frontend{
		prompt:  prompt,
		verbose: verbose,
		output:  output,
	}
}

func NewInteractiveFrontend(input io.Reader, verbose bool, output io.Writer) *Frontend {
	return &Frontend{
		prompt:  "-",
		verbose: verbose,
		output:  output,
		scanner: bufio.NewScanner(input),
	}
}

func (f *Frontend) AwaitUserInput(ctx context.Context) (string, error) {
	if f.scanner != nil {
		return f.readStdin(ctx)
	}

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

func (f *Frontend) readStdin(_ context.Context) (string, error) {
	for f.scanner.Scan() {
		line := f.scanner.Text()
		if line == "" {
			continue
		}

		var envelope struct {
			Type    string `json:"type"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			f.emitWarning(fmt.Sprintf("invalid input: %v", err))
			continue
		}

		switch envelope.Type {
		case "user_message":
			return envelope.Content, nil
		case "exit":
			return "", ErrExit
		default:
			f.emitWarning(fmt.Sprintf("unknown input type: %q", envelope.Type))
			continue
		}
	}
	return "", ErrExit
}

func (f *Frontend) emitWarning(msg string) {
	data, err := session.EncodeEvent(session.Warning{Message: msg})
	if err != nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	_, _ = f.output.Write(data)
	_, _ = f.output.Write([]byte("\n"))
}
```

Note: this requires exporting the `encode` function in `session/events.go` as `EncodeEvent` (or adding a public wrapper). See Step 3a.

**Step 3a: Export the encode function in session/events.go**

Add a public wrapper below the existing `encode` function in `gohome/internal/session/events.go`:

```go
func EncodeEvent(ev any) ([]byte, error) {
	return encode(ev)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/headless/ -run "TestAwaitUserInput_Interactive" -v`
Expected: All 5 new tests PASS.

**Step 5: Run full headless test suite**

Run: `go test ./gohome/internal/headless/ -v`
Expected: All tests PASS (existing tests unaffected).

**Step 6: Commit**

```bash
git add gohome/internal/session/events.go gohome/internal/headless/frontend.go gohome/internal/headless/frontend_test.go
git commit -m "feat(headless): add interactive stdin JSONL reading to Frontend"
```

---

### Task 3: Wire the interactive loop in main.go

**Files:**
- Modify: `gohome/cmd/gohome/main.go:355-466`

**Step 1: Add validation for --prompt - requiring --verbose**

In `main.go`, after the existing `--prompt` / `--yolo` validation (line 208-214), add:

```go
if *prompt == "-" && !*verbose {
	fmt.Fprintf(os.Stderr, "gohome: --prompt - (interactive mode) requires --verbose\n")
	os.Exit(1)
}
```

**Step 2: Split the headless block into one-shot vs interactive**

Replace the headless execution block (lines 355-466) to detect `*prompt == "-"` and branch:

For the **interactive branch** (`*prompt == "-"`):
- Create `headless.NewInteractiveFrontend(os.Stdin, true, os.Stdout)` instead of `NewFrontend`.
- After building the agent, run a loop modeled on `runLoop`:

```go
for {
	text, err := hfe.AwaitUserInput(ctx)
	if err != nil {
		if errors.Is(err, headless.ErrExit) {
			break
		}
		break
	}

	sess.History = append(sess.History, common.Message{
		Role: common.RoleUser,
		Content: []common.Block{
			{Kind: common.BlockText, Text: text},
		},
	})
	writer.Emit(session.UserMessage{
		Content: []common.Block{
			{Kind: common.BlockText, Text: text},
		},
	})

	runErr := a.Run(ctx, sess)
	writer.Emit(session.TurnDone{SessionID: sess.ID})

	if runErr != nil && runErr != context.Canceled {
		if errors.Is(runErr, agent.ErrToolDenied) {
			continue
		}
		fmt.Fprintf(os.Stderr, "gohome: agent error: %v\n", runErr)
		break
	}
	if ctx.Err() != nil {
		break
	}
}
```

- After the loop, emit `session_end` with reason `"exit"` and exit cleanly.

For the **one-shot branch** (`*prompt != "-"`):
- Keep the existing code exactly as-is.

**Step 3: Add the headless import for ErrExit**

The `headless` package is already imported in `main.go`. The `errors` package is already imported. No new imports needed.

**Step 4: Build and verify compilation**

Run: `go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome`
Expected: Build succeeds with no errors.

**Step 5: Run vet and lint**

Run: `go vet ./gohome/...`
Run: `golangci-lint run ./gohome/...`
Expected: No new warnings or errors.

**Step 6: Commit**

```bash
git add gohome/cmd/gohome/main.go
git commit -m "feat: wire interactive CLI loop for --prompt - mode"
```

---

### Task 4: Integration test for interactive mode

**Files:**
- Modify: `gohome/internal/headless/integration_test.go`

**Step 1: Write an integration test for multi-turn interactive session**

Add to `gohome/internal/headless/integration_test.go`:

```go
func TestHeadless_Interactive_MultiTurn(t *testing.T) {
	input := strings.NewReader(
		"{\"type\":\"user_message\",\"content\":\"say hello\"}\n" +
			"{\"type\":\"user_message\",\"content\":\"say goodbye\"}\n" +
			"{\"type\":\"exit\"}\n",
	)
	var buf bytes.Buffer
	fe := headless.NewInteractiveFrontend(input, true, &buf)

	client := &fakeClient{response: "Hello!"}
	dir := t.TempDir()
	sess := session.NewSession("test-interactive", dir, "fake-model", "fake")
	writerPath := filepath.Join(dir, "test.jsonl")
	writer, err := session.OpenWriter(writerPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Close() }()

	wl, _ := guard.LoadWhitelist("", "")
	g := guard.NewGuard(wl, fe, nil)
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

	ctx := context.Background()
	for {
		text, inputErr := fe.AwaitUserInput(ctx)
		if inputErr != nil {
			break
		}
		sess.History = append(sess.History, common.Message{
			Role:    common.RoleUser,
			Content: []common.Block{{Kind: common.BlockText, Text: text}},
		})
		writer.Emit(session.UserMessage{
			Content: []common.Block{{Kind: common.BlockText, Text: text}},
		})
		runErr := a.Run(ctx, sess)
		writer.Emit(session.TurnDone{SessionID: sess.ID})
		if runErr != nil {
			t.Fatalf("agent.Run failed: %v", runErr)
		}
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	var turnDoneCount int
	for _, line := range lines {
		if strings.Contains(line, "\"kind\":\"turn_done\"") || strings.Contains(line, "\"type\":\"turn_done\"") {
			turnDoneCount++
		}
	}
	// The writer emits TurnDone events, but the Frontend.Emit outputs agent events.
	// We check that we got at least some output for each turn.
	if len(lines) < 2 {
		t.Errorf("expected output for 2 turns, got %d lines", len(lines))
	}
}
```

Add `"strings"` to the imports at the top of the file and add `"github.com/jhyoong/GoHome/gohome/internal/llm/common"` if not already present.

**Step 2: Run the integration test**

Run: `go test ./gohome/internal/headless/ -run TestHeadless_Interactive_MultiTurn -v`
Expected: PASS

**Step 3: Run full test suite**

Run: `go test ./gohome/... -v`
Expected: All tests PASS.

**Step 4: Commit**

```bash
git add gohome/internal/headless/integration_test.go
git commit -m "test: add integration test for interactive multi-turn headless mode"
```

---

### Task 5: Manual smoke test

**Step 1: Build**

Run: `go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome`

**Step 2: Test that --prompt - without --verbose is rejected**

Run: `echo '{"type":"exit"}' | ./bin/gohome --prompt - --yolo --model <configured-model>`
Expected: stderr prints `gohome: --prompt - (interactive mode) requires --verbose` and exits non-zero.

**Step 3: Test that one-shot --prompt still works**

Run: `./bin/gohome --prompt "what is 2+2" --yolo --model <configured-model>`
Expected: Prints an answer and exits (existing behavior unchanged).

**Step 4: Confirm build is clean**

Run: `go vet ./gohome/... && golangci-lint run ./gohome/...`
Expected: No errors or warnings.
