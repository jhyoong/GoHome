# Sudo Password Handling Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Detect sudo commands in the shell tool, collect the user's password via the approval prompt, pipe it to sudo -S, and cache it per session.

**Architecture:** The guard layer detects sudo in shell commands and flags the ApprovalRequest. The TUI adds a masked password field to the approval overlay. The password flows back through the Decision to the agent, which passes it to the shell tool via context. The shell tool rewrites the command to use `sudo -S` and pipes the password to stdin. The TUI caches the password in memory after a successful sudo.

**Tech Stack:** Go, Bubble Tea (charmbracelet/bubbletea), charmbracelet/bubbles/textinput

---

### Task 1: Sudo detection helper in guard package

**Files:**
- Create: `gohome/internal/guard/sudo.go`
- Test: `gohome/internal/guard/sudo_test.go`

**Step 1: Write the failing tests**

In `gohome/internal/guard/sudo_test.go`:

```go
package guard

import "testing"

func TestIsSudoCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{"plain sudo", "sudo apt install vim", true},
		{"sudo with path", "sudo /usr/bin/apt install vim", true},
		{"piped sudo", "echo foo | sudo tee /etc/bar", true},
		{"chained sudo", "cd /tmp && sudo rm -rf stuff", true},
		{"semicolon sudo", "echo hi; sudo ls", true},
		{"or sudo", "test -f foo || sudo install foo", true},
		{"not sudo", "echo sudoers", false},
		{"grep sudoers", "grep sudo /etc/sudoers", false},
		{"empty", "", false},
		{"no sudo", "ls -la", false},
		{"sudo alone", "sudo", true},
		{"sudo-S already", "sudo -S apt install vim", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSudoCommand(tt.command); got != tt.want {
				t.Errorf("IsSudoCommand(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/guard/ -run TestIsSudoCommand -v`
Expected: FAIL with "undefined: IsSudoCommand"

**Step 3: Write minimal implementation**

In `gohome/internal/guard/sudo.go`:

```go
package guard

import "regexp"

var sudoRe = regexp.MustCompile(`(^|[;&|]\s*)sudo(\s|$)`)

// IsSudoCommand reports whether the shell command string contains a sudo
// invocation at a command boundary.
func IsSudoCommand(command string) bool {
	return sudoRe.MatchString(command)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/guard/ -run TestIsSudoCommand -v`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/guard/sudo.go gohome/internal/guard/sudo_test.go
git commit -m "feat: add IsSudoCommand detection helper in guard package"
```

---

### Task 2: Add sudo fields to ApprovalRequest and ApprovalDecision

**Files:**
- Modify: `gohome/internal/guard/guard.go` (lines 15-29)
- Modify: `gohome/internal/guard/check.go` (lines 46-56)

**Step 1: Write the failing test**

In `gohome/internal/guard/guard_test.go`, add at the end:

```go
func TestCheck_SudoCommand_SetsNeedsSudoPassword(t *testing.T) {
	fe := &fakeFrontend{
		response: ApprovalDecision{Outcome: AllowOnce},
	}
	g := newTestGuard(emptyWhitelist(t), fe)

	_, _ = g.Check(context.Background(), "sess1", "shell", bashCmd("sudo apt install vim"))
	if !fe.lastReq.NeedsSudoPassword {
		t.Error("expected NeedsSudoPassword=true for sudo command")
	}
}

func TestCheck_NonSudoCommand_DoesNotSetNeedsSudoPassword(t *testing.T) {
	fe := &fakeFrontend{
		response: ApprovalDecision{Outcome: AllowOnce},
	}
	g := newTestGuard(emptyWhitelist(t), fe)

	_, _ = g.Check(context.Background(), "sess1", "shell", bashCmd("ls -la"))
	if fe.lastReq.NeedsSudoPassword {
		t.Error("expected NeedsSudoPassword=false for non-sudo command")
	}
}

func TestCheck_SudoPassword_ThreadedToDecision(t *testing.T) {
	fe := &fakeFrontend{
		response: ApprovalDecision{Outcome: AllowOnce, SudoPassword: "secret123"},
	}
	g := newTestGuard(emptyWhitelist(t), fe)

	dec, err := g.Check(context.Background(), "sess1", "shell", bashCmd("sudo apt install vim"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.SudoPassword != "secret123" {
		t.Errorf("SudoPassword = %q, want %q", dec.SudoPassword, "secret123")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/guard/ -run TestCheck_Sudo -v`
Expected: FAIL with compile errors (fields don't exist yet)

**Step 3: Write minimal implementation**

In `gohome/internal/guard/guard.go`, add `NeedsSudoPassword` to `ApprovalRequest` and `SudoPassword` to both `ApprovalDecision` and `Decision`:

```go
type ApprovalRequest struct {
	SessionID        string
	Tool             string
	Input            json.RawMessage
	Summary          string
	SuggestedPattern string
	NeedsSudoPassword bool
}

type ApprovalDecision struct {
	Outcome      ApprovalOutcome
	SavedPattern string
	SteerMessage string
	SudoPassword string
}

type Decision struct {
	Allow        bool
	Reason       string
	SteerMessage string
	SavedPattern string
	SudoPassword string
}
```

In `gohome/internal/guard/check.go`, detect sudo when building the approval request (around line 47) and thread `SudoPassword` through in the `AllowOnce` and `AllowAlways` branches:

Replace the approval request construction block (lines 47-54) with:

```go
	summary := summaryFor(tool, input)
	needsSudo := false
	if tool == "shell" {
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(input, &args); err == nil {
			needsSudo = IsSudoCommand(args.Command)
		}
	}
	req := ApprovalRequest{
		SessionID:         sessionID,
		Tool:              tool,
		Input:             input,
		Summary:           summary,
		SuggestedPattern:  Suggest(tool, input),
		NeedsSudoPassword: needsSudo,
	}
```

In the switch on `dec.Outcome`, thread `SudoPassword` into the returned `Decision` for `AllowOnce` and `AllowAlways`:

```go
	case AllowOnce:
		return Decision{Allow: true, Reason: "user_once", SudoPassword: dec.SudoPassword}, nil

	case AllowAlways:
		if err := g.whitelist.AddProject(tool, dec.SavedPattern); err != nil {
			slog.Warn("whitelist persist failed", "err", err)
		}
		return Decision{Allow: true, Reason: "user_always", SavedPattern: dec.SavedPattern, SudoPassword: dec.SudoPassword}, nil
```

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/guard/ -run TestCheck_Sudo -v`
Expected: PASS

**Step 5: Run all guard tests to check for regressions**

Run: `go test ./gohome/internal/guard/ -v`
Expected: All PASS

**Step 6: Commit**

```bash
git add gohome/internal/guard/guard.go gohome/internal/guard/check.go gohome/internal/guard/guard_test.go
git commit -m "feat: add NeedsSudoPassword and SudoPassword fields to guard types"
```

---

### Task 3: Context-based sudo password threading in tools package

**Files:**
- Create: `gohome/internal/tools/sudo.go`
- Test: `gohome/internal/tools/sudo_test.go`

This task adds a context key for passing the sudo password from the agent layer into the shell tool, following the same pattern as `WithSession`/`SessionFrom` in `gohome/internal/tools/session.go`.

**Step 1: Write the failing tests**

In `gohome/internal/tools/sudo_test.go`:

```go
package tools

import (
	"context"
	"testing"
)

func TestSudoPasswordFromContext(t *testing.T) {
	ctx := WithSudoPassword(context.Background(), "mypassword")
	got := SudoPasswordFrom(ctx)
	if got != "mypassword" {
		t.Errorf("SudoPasswordFrom = %q, want %q", got, "mypassword")
	}
}

func TestSudoPasswordFromContext_Empty(t *testing.T) {
	got := SudoPasswordFrom(context.Background())
	if got != "" {
		t.Errorf("SudoPasswordFrom on bare context = %q, want empty", got)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/tools/ -run TestSudoPassword -v`
Expected: FAIL with "undefined: WithSudoPassword"

**Step 3: Write minimal implementation**

In `gohome/internal/tools/sudo.go`:

```go
package tools

import "context"

type sudoPasswordKey struct{}

// WithSudoPassword stores the sudo password in ctx.
func WithSudoPassword(ctx context.Context, password string) context.Context {
	return context.WithValue(ctx, sudoPasswordKey{}, password)
}

// SudoPasswordFrom retrieves the sudo password from ctx.
// Returns "" if no password is present.
func SudoPasswordFrom(ctx context.Context) string {
	s, _ := ctx.Value(sudoPasswordKey{}).(string)
	return s
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/tools/ -run TestSudoPassword -v`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/tools/sudo.go gohome/internal/tools/sudo_test.go
git commit -m "feat: add WithSudoPassword/SudoPasswordFrom context helpers"
```

---

### Task 4: Shell tool reads sudo password from context and pipes to stdin

**Files:**
- Modify: `gohome/internal/tools/shell.go` (lines 60-167)
- Modify: `gohome/internal/tools/shell_test.go`

**Step 1: Write the failing tests**

Add to `gohome/internal/tools/shell_test.go`:

```go
func TestBash_SudoPasswordPipedToStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	// Use a command that reads from stdin and echoes it, simulating sudo -S behavior.
	// "head -1" reads one line from stdin.
	ctx := WithSudoPassword(context.Background(), "testpass")
	raw, _ := json.Marshal(map[string]any{"command": "head -1"})
	bt := &ShellTool{}
	res, err := bt.Execute(ctx, raw, NullSink{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected IsError: %s", res.Content)
	}
	if !strings.Contains(res.Content, "testpass") {
		t.Errorf("expected stdin password in output, got %q", res.Content)
	}
}

func TestBash_NoSudoPassword_StdinEmpty(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	// Without a sudo password in context, stdin should be empty (read returns EOF).
	raw, _ := json.Marshal(map[string]any{"command": "cat"})
	bt := &ShellTool{}
	res, err := bt.Execute(context.Background(), raw, NullSink{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected IsError: %s", res.Content)
	}
	// cat with no stdin should produce empty output (just "exit 0").
	if strings.Contains(res.Content, "testpass") {
		t.Errorf("should not contain password without context, got %q", res.Content)
	}
}

func TestInjectSudoS(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain sudo", "sudo apt install vim", "sudo -S apt install vim"},
		{"sudo already has -S", "sudo -S apt install vim", "sudo -S apt install vim"},
		{"piped sudo", "echo foo | sudo tee /etc/bar", "echo foo | sudo -S tee /etc/bar"},
		{"chained sudo", "cd /tmp && sudo rm -rf stuff", "cd /tmp && sudo -S rm -rf stuff"},
		{"no sudo", "ls -la", "ls -la"},
		{"sudo with flags", "sudo -u root ls", "sudo -S -u root ls"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := injectSudoS(tt.in)
			if got != tt.want {
				t.Errorf("injectSudoS(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/tools/ -run "TestBash_SudoPassword|TestBash_NoSudoPassword|TestInjectSudoS" -v`
Expected: FAIL with "undefined: injectSudoS"

**Step 3: Write minimal implementation**

In `gohome/internal/tools/shell.go`, add the `injectSudoS` helper and modify `Execute` to read the password from context:

Add the `injectSudoS` function (before `Execute` or at end of file):

```go
var sudoWordRe = regexp.MustCompile(`(^|(?:[;&|]\s*))sudo\s`)

// injectSudoS rewrites "sudo" to "sudo -S" at command boundaries, unless
// -S is already present immediately after sudo.
func injectSudoS(command string) string {
	return sudoWordRe.ReplaceAllStringFunc(command, func(match string) string {
		if strings.Contains(match, "-S") {
			return match
		}
		// Replace "sudo " with "sudo -S " preserving any leading separator.
		return strings.Replace(match, "sudo ", "sudo -S ", 1)
	})
}
```

Add `"regexp"` to the imports.

In the `Execute` method, after building `cmd` (after line 95 `cmd.Dir = *inp.CWD`), add the sudo stdin piping:

```go
	// If a sudo password is available in context, pipe it to stdin and inject -S.
	sudoPassword := SudoPasswordFrom(ctx)
	if sudoPassword != "" {
		cmd.Stdin = strings.NewReader(sudoPassword + "\n")
		if runtime.GOOS != "windows" {
			cmd.Args[len(cmd.Args)-1] = injectSudoS(inp.Command)
		}
	}
```

This must go after `cmd` is built and before the `io.Pipe()` for stdout/stderr (before current line 98).

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/tools/ -run "TestBash_SudoPassword|TestBash_NoSudoPassword|TestInjectSudoS" -v`
Expected: PASS

**Step 5: Run all shell tests to check for regressions**

Run: `go test ./gohome/internal/tools/ -v`
Expected: All PASS

**Step 6: Commit**

```bash
git add gohome/internal/tools/shell.go gohome/internal/tools/shell_test.go
git commit -m "feat: shell tool pipes sudo password from context to stdin with -S injection"
```

---

### Task 5: Agent threads sudo password from guard Decision into tool context

**Files:**
- Modify: `gohome/internal/agent/run.go` (lines 163-175)

**Step 1: Write the failing test**

Add to `gohome/internal/agent/run_test.go`. The test should verify that when the guard decision contains a SudoPassword, the tool receives it in its context. Use a spy tool:

```go
func TestDispatchTool_SudoPasswordInContext(t *testing.T) {
	reg := tools.NewRegistry()
	var capturedCtx context.Context
	reg.Register(&spyContextTool{
		onExecute: func(ctx context.Context) {
			capturedCtx = ctx
		},
	})

	wl, err := guard.Compile(guard.WhitelistFile{}, guard.WhitelistFile{}, "")
	if err != nil {
		t.Fatal(err)
	}
	fe := &stubFrontend{
		approval: guard.ApprovalDecision{
			Outcome:      guard.AllowOnce,
			SudoPassword: "secret123",
		},
	}
	g := guard.NewGuard(wl, fe)
	a := &Agent{
		Guard: g,
		Tools: reg,
		State: newState(),
	}

	sess := &session.Session{ID: "test"}
	block := common.Block{
		Kind:      common.BlockToolUse,
		ToolUseID: "tu1",
		ToolName:  "spy",
		InputJSON: `{}`,
	}

	ctx := context.Background()
	tctx := tools.WithSession(ctx, sess)
	a.dispatchTool(ctx, tctx, sess, block)

	if capturedCtx == nil {
		t.Fatal("tool was not executed")
	}
	got := tools.SudoPasswordFrom(capturedCtx)
	if got != "secret123" {
		t.Errorf("SudoPasswordFrom = %q, want %q", got, "secret123")
	}
}
```

You will also need the `spyContextTool` type and `stubFrontend` if they don't already exist in `run_test.go`. Check if similar test doubles exist and reuse them. If not:

```go
type spyContextTool struct {
	onExecute func(ctx context.Context)
}

func (s *spyContextTool) Name() string                    { return "spy" }
func (s *spyContextTool) Description() string             { return "spy tool" }
func (s *spyContextTool) InputSchema() json.RawMessage    { return json.RawMessage(`{}`) }
func (s *spyContextTool) Execute(ctx context.Context, in json.RawMessage, sink tools.ProgressSink) (tools.Result, error) {
	if s.onExecute != nil {
		s.onExecute(ctx)
	}
	return tools.Result{Content: "ok"}, nil
}
```

Check the existing test file first -- there may already be a `stubFrontend` or equivalent helper you can reuse.

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/agent/ -run TestDispatchTool_SudoPasswordInContext -v`
Expected: FAIL (spyContextTool may compile but SudoPassword not threaded)

**Step 3: Write minimal implementation**

In `gohome/internal/agent/run.go`, in the `dispatchTool` method, after the guard check succeeds and before tool execution (around line 169), inject the sudo password into the tool's context:

Replace:
```go
	start := time.Now()
	res, execErr := safeExecute(tctx, tool, input, tools.NullSink{})
```

With:
```go
	execCtx := tctx
	if dec.SudoPassword != "" {
		execCtx = tools.WithSudoPassword(tctx, dec.SudoPassword)
	}

	start := time.Now()
	res, execErr := safeExecute(execCtx, tool, input, tools.NullSink{})
```

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/agent/ -run TestDispatchTool_SudoPasswordInContext -v`
Expected: PASS

**Step 5: Run all agent tests to check for regressions**

Run: `go test ./gohome/internal/agent/ -v`
Expected: All PASS

**Step 6: Commit**

```bash
git add gohome/internal/agent/run.go gohome/internal/agent/run_test.go
git commit -m "feat: thread sudo password from guard Decision into tool execution context"
```

---

### Task 6: TUI approval prompt shows masked password field for sudo commands

**Files:**
- Modify: `gohome/internal/tui/approval.go` (lines 24-58, 79-121)
- Modify: `gohome/internal/tui/model_approval.go` (lines 96-131)
- Test: `gohome/internal/tui/approval_test.go`

**Step 1: Write the failing tests**

Add to `gohome/internal/tui/approval_test.go`:

```go
func TestApprovalSudoPasswordFieldShown(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	ch := make(chan guard.ApprovalDecision, 1)
	msg := tui.ApprovalReqMsg{
		Req: guard.ApprovalRequest{
			SessionID:         "main",
			Tool:              "shell",
			Input:             json.RawMessage(`{"command":"sudo apt install vim"}`),
			SuggestedPattern:  "^sudo",
			NeedsSudoPassword: true,
		},
		Reply: ch,
	}
	tm.Send(msg)

	// The overlay should show a password prompt.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Password:"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

func TestApprovalSudoPasswordIncludedInDecision(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	ch := make(chan guard.ApprovalDecision, 1)
	msg := tui.ApprovalReqMsg{
		Req: guard.ApprovalRequest{
			SessionID:         "main",
			Tool:              "shell",
			Input:             json.RawMessage(`{"command":"sudo apt install vim"}`),
			SuggestedPattern:  "^sudo",
			NeedsSudoPassword: true,
		},
		Reply: ch,
	}
	tm.Send(msg)

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Password:"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Type the password (masked, so we just type characters).
	tm.Type("mysecret")
	// Press '1' to allow once.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})

	select {
	case dec := <-ch:
		if dec.Outcome != guard.AllowOnce {
			t.Fatalf("expected AllowOnce, got %q", dec.Outcome)
		}
		if dec.SudoPassword != "mysecret" {
			t.Fatalf("expected SudoPassword %q, got %q", "mysecret", dec.SudoPassword)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for decision")
	}
}

func TestApprovalNonSudoNoPasswordField(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	msg, ch := makeApprovalReq("main", "shell", "^ls", json.RawMessage(`{"command":"ls"}`))
	tm.Send(msg)

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("shell: ls"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Press '1' to allow once -- no password should be in the decision.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})

	select {
	case dec := <-ch:
		if dec.SudoPassword != "" {
			t.Fatalf("expected empty SudoPassword for non-sudo, got %q", dec.SudoPassword)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/tui/ -run "TestApprovalSudo" -v`
Expected: FAIL

**Step 3: Write minimal implementation**

**`approval.go` changes:**

Add `passwordInput` field and `needsSudo` flag to `approvalPrompt`:

```go
type approvalPrompt struct {
	req     guard.ApprovalRequest
	reply   chan guard.ApprovalDecision
	pattern string

	selected int

	editing      bool
	patternInput textinput.Model

	steering   bool
	steerInput textinput.Model

	needsSudo     bool
	passwordInput textinput.Model
}
```

In `newApprovalPrompt`, initialize the password field when `req.NeedsSudoPassword` is true:

```go
func newApprovalPrompt(req guard.ApprovalRequest, reply chan guard.ApprovalDecision) *approvalPrompt {
	pi := textinput.New()
	pi.Placeholder = "pattern"
	pi.SetValue(req.SuggestedPattern)

	si := textinput.New()
	si.Placeholder = "steer message"

	pwi := textinput.New()
	pwi.Placeholder = ""
	pwi.EchoMode = textinput.EchoPassword
	if req.NeedsSudoPassword {
		pwi.Focus()
	}

	return &approvalPrompt{
		req:           req,
		reply:         reply,
		pattern:       req.SuggestedPattern,
		patternInput:  pi,
		steerInput:    si,
		needsSudo:     req.NeedsSudoPassword,
		passwordInput: pwi,
	}
}
```

In `renderApprovalOverlay`, render the password field below the summary when `needsSudo` is true. Insert after `sb.WriteString(approvalSummaryLine(ap, focusedSessionID))` and `sb.WriteString("\n")`:

```go
	if ap.needsSudo {
		sb.WriteString("Password: ")
		sb.WriteString(ap.passwordInput.View())
		sb.WriteString("\n")
	}
```

**`model_approval.go` changes:**

The password field needs to receive key input when it's focused. When `needsSudo` is true and the user is not in steering or editing mode, key presses should go to the password input -- except for the menu selection keys (1-4, arrows, Enter, Esc, e).

In `handleApprovalKey`, in the top-level menu section, route non-menu keys to the password input when `needsSudo` is true. Add a default case at the end of the top-level switch:

```go
	default:
		if ap.needsSudo {
			var tiCmd tea.Cmd
			ap.passwordInput, tiCmd = ap.passwordInput.Update(msg)
			cmds = append(cmds, tiCmd)
		}
```

When resolving approvals (AllowOnce / AllowAlways), include the password from the input:

For every call to `m.resolveApproval(guard.ApprovalDecision{Outcome: guard.AllowOnce})`, change to:

```go
m.resolveApproval(m.buildApprovalDecision(guard.AllowOnce))
```

Add a helper method on `Model`:

```go
func (m *Model) buildApprovalDecision(outcome guard.ApprovalOutcome) guard.ApprovalDecision {
	dec := guard.ApprovalDecision{Outcome: outcome}
	if m.activeApproval != nil && m.activeApproval.needsSudo {
		dec.SudoPassword = m.activeApproval.passwordInput.Value()
	}
	if outcome == guard.AllowAlways && m.activeApproval != nil {
		dec.SavedPattern = m.activeApproval.pattern
	}
	return dec
}
```

Then update all the `resolveApproval` calls in `handleApprovalKey` to use `m.buildApprovalDecision(...)` for AllowOnce and AllowAlways outcomes, keeping Deny/DenySteer as-is (they don't need a password).

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/tui/ -run "TestApprovalSudo" -v`
Expected: PASS

**Step 5: Run all TUI tests to check for regressions**

Run: `go test ./gohome/internal/tui/ -v`
Expected: All PASS

**Step 6: Commit**

```bash
git add gohome/internal/tui/approval.go gohome/internal/tui/model_approval.go gohome/internal/tui/approval_test.go
git commit -m "feat: show masked password field in approval prompt for sudo commands"
```

---

### Task 7: Per-session sudo password caching in TUI

**Files:**
- Modify: `gohome/internal/tui/model.go` (add `sudoPasswordCache` field, around line 167)
- Modify: `gohome/internal/tui/model_approval.go` (auto-fill from cache, update cache on success)
- Modify: `gohome/internal/tui/model_agent.go` (update cache on successful tool result)
- Test: `gohome/internal/tui/approval_test.go`

**Step 1: Write the failing test**

Add to `gohome/internal/tui/approval_test.go`:

```go
func TestApprovalSudoPasswordCachedOnSuccess(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	// First sudo command -- user types password.
	ch1 := make(chan guard.ApprovalDecision, 1)
	msg1 := tui.ApprovalReqMsg{
		Req: guard.ApprovalRequest{
			SessionID:         "main",
			Tool:              "shell",
			Input:             json.RawMessage(`{"command":"sudo apt install vim"}`),
			SuggestedPattern:  "^sudo",
			NeedsSudoPassword: true,
		},
		Reply: ch1,
	}
	tm.Send(msg1)

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Password:"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Type("cached_pass")
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})

	select {
	case dec := <-ch1:
		if dec.SudoPassword != "cached_pass" {
			t.Fatalf("first sudo: expected password %q, got %q", "cached_pass", dec.SudoPassword)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first decision")
	}

	// Simulate a successful tool result to trigger cache update.
	tm.Send(tui.AgentEventMsg{
		SessionID: "main",
		Ev: agent.Event{
			Kind:      agent.EventToolResult,
			SessionID: "main",
			Result: &agent.ToolResult{
				Content: "exit 0\npackage installed",
				IsError: false,
			},
		},
	})

	// Second sudo command -- password should be pre-filled from cache.
	ch2 := make(chan guard.ApprovalDecision, 1)
	msg2 := tui.ApprovalReqMsg{
		Req: guard.ApprovalRequest{
			SessionID:         "main",
			Tool:              "shell",
			Input:             json.RawMessage(`{"command":"sudo systemctl restart nginx"}`),
			SuggestedPattern:  "^sudo",
			NeedsSudoPassword: true,
		},
		Reply: ch2,
	}
	tm.Send(msg2)

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Password:"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Don't type anything -- just approve. The cached password should be used.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})

	select {
	case dec := <-ch2:
		if dec.SudoPassword != "cached_pass" {
			t.Fatalf("second sudo: expected cached password %q, got %q", "cached_pass", dec.SudoPassword)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second decision")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/tui/ -run TestApprovalSudoPasswordCachedOnSuccess -v`
Expected: FAIL

**Step 3: Write minimal implementation**

**`model.go`:** Add `sudoPasswordCache string` field to the `Model` struct (add near line 167, after `lastCtrlC`):

```go
	sudoPasswordCache string
```

**`model_approval.go`:** In `handleApprovalReq`, when building an approval prompt for a sudo command, pre-fill the password from cache:

After `ap := newApprovalPrompt(msg.Req, msg.Reply)`, add:

```go
	if ap.needsSudo && m.sudoPasswordCache != "" {
		ap.passwordInput.SetValue(m.sudoPasswordCache)
	}
```

In `resolveApproval`, when the outcome is AllowOnce or AllowAlways and the prompt has a sudo password, save it to cache:

After `m.activeApproval.reply <- dec`, add:

```go
	if m.activeApproval.needsSudo && dec.SudoPassword != "" {
		m.sudoPasswordCache = dec.SudoPassword
	}
```

Note: We cache optimistically on approve. The design says "after a successful sudo (exit 0)" but checking exit code requires correlating the tool result back to a specific sudo command. A simpler approach that still works: cache whenever the user provides a password. If sudo fails with a wrong password, the user will see the error and can type a new one on the next prompt.

Alternatively, to match the design doc exactly, track a `lastSudoPassword` on Model and promote it to `sudoPasswordCache` when the corresponding tool result comes back with exit code 0. This requires:

**`model.go`:** Add `pendingSudoPassword string` alongside `sudoPasswordCache`.

**`model_approval.go`:** On resolve, store to `pendingSudoPassword` instead of `sudoPasswordCache`.

**`model_agent.go`:** In the `EventToolResult` case, check if `m.pendingSudoPassword != ""` and the result content starts with `"exit 0"`:

```go
	if m.pendingSudoPassword != "" && !isErr && strings.HasPrefix(content, "exit 0") {
		m.sudoPasswordCache = m.pendingSudoPassword
	}
	m.pendingSudoPassword = ""
```

Use whichever approach the implementer prefers. The simpler "cache on approve" is recommended unless testing reveals issues.

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/tui/ -run TestApprovalSudoPasswordCachedOnSuccess -v`
Expected: PASS

**Step 5: Run all TUI tests to check for regressions**

Run: `go test ./gohome/internal/tui/ -v`
Expected: All PASS

**Step 6: Commit**

```bash
git add gohome/internal/tui/model.go gohome/internal/tui/model_approval.go gohome/internal/tui/model_agent.go gohome/internal/tui/approval_test.go
git commit -m "feat: cache sudo password per session and auto-fill on subsequent prompts"
```

---

### Task 8: Ensure password is never persisted to session JSONL or logs

**Files:**
- Test: `gohome/internal/session/events_test.go`
- Test: `gohome/internal/guard/guard_test.go`

This is a verification-only task. The password should never appear in serialized session events or debug logs. The Approval event struct in `session/events.go` has no SudoPassword field, so by construction the password cannot be persisted. Write a test to lock this invariant.

**Step 1: Write the verification test**

Add to `gohome/internal/session/events_test.go`:

```go
func TestApprovalEvent_NoPasswordField(t *testing.T) {
	ev := Approval{
		ToolUseID: "tu1",
		Outcome:   "user_once",
	}
	data, err := encode(ev)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Contains(string(data), "password") || strings.Contains(string(data), "Password") {
		t.Errorf("approval event JSON must not contain password field: %s", data)
	}
}
```

**Step 2: Run test**

Run: `go test ./gohome/internal/session/ -run TestApprovalEvent_NoPasswordField -v`
Expected: PASS (this confirms the invariant holds)

**Step 3: Commit**

```bash
git add gohome/internal/session/events_test.go
git commit -m "test: verify approval events never serialize password data"
```

---

### Task 9: Update golden snapshot tests

**Files:**
- Modify: `gohome/internal/tui/tui_snapshot_test.go` (add a sudo approval snapshot case)
- Update: golden files in `gohome/internal/tui/testdata/`

**Step 1: Add a snapshot test case for sudo approval**

Add to `tui_snapshot_test.go` alongside the existing snapshot sub-tests:

```go
	t.Run("sudo_approval_prompt", func(t *testing.T) {
		m := tui.New(nil, "sess1")
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		ch := make(chan guard.ApprovalDecision, 1)
		m.Update(tui.ApprovalReqMsg{
			Req: guard.ApprovalRequest{
				SessionID:         "sess1",
				Tool:              "shell",
				Input:             json.RawMessage(`{"command":"sudo apt install vim"}`),
				SuggestedPattern:  "^sudo",
				NeedsSudoPassword: true,
			},
			Reply: ch,
		})
		got := m.View()
		golden.RequireEqual(t, []byte(got))
	})
```

**Step 2: Generate the golden file**

Run: `go test ./gohome/internal/tui/ -run "TestSnapshots/sudo_approval_prompt" -update`

**Step 3: Verify the snapshot passes**

Run: `go test ./gohome/internal/tui/ -run "TestSnapshots/sudo_approval_prompt" -v`
Expected: PASS

**Step 4: Visually inspect the golden file**

Read the generated golden file at `gohome/internal/tui/testdata/TestSnapshots/sudo_approval_prompt.golden` and confirm:
- The "Password:" label and masked input field appear
- The standard approval menu (1-4) is visible below it
- No actual password text is shown

**Step 5: Commit**

```bash
git add gohome/internal/tui/tui_snapshot_test.go gohome/internal/tui/testdata/
git commit -m "test: add golden snapshot for sudo approval prompt overlay"
```

---

### Task 10: Full integration verification

**Step 1: Run all package tests**

```bash
go test ./gohome/... -v
```

Expected: All PASS

**Step 2: Run linter**

```bash
golangci-lint run ./gohome/...
```

Expected: No new warnings

**Step 3: Run vet**

```bash
go vet ./gohome/...
```

Expected: Clean

**Step 4: Build**

```bash
go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome
```

Expected: Build succeeds

**Step 5: Commit any final fixes**

If any test or lint issues are found, fix and commit:

```bash
git commit -m "fix: address lint/test issues from sudo password feature"
```
