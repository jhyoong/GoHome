# Token Tracking Fix, CWD in Statusbar, Send Feedback Spinner -- Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix three TUI issues: cumulative token tracking, CWD display in statusbar, and immediate spinner feedback on message send.

**Architecture:** All three changes are localized to the TUI package (`gohome/internal/tui/`), with one small wiring addition in `main.go`. No new packages or interfaces.

**Tech Stack:** Go, Bubble Tea (charmbracelet/bubbletea), golden-file snapshot tests.

---

### Task 1: Fix cumulative token tracking

**Files:**
- Modify: `gohome/internal/tui/model_agent.go:122-126`
- Test: `gohome/internal/tui/statusbar_test.go` (new test)

**Step 1: Write the failing test**

Add a test to `gohome/internal/tui/statusbar_test.go` that sends two `EventUsageUpdated` events and asserts the accumulated total appears in the output.

```go
// TestStatusBarCumulativeTokens verifies that multiple usage events accumulate
// rather than replacing each other.
func TestStatusBarCumulativeTokens(t *testing.T) {
	m := tui.New(nil, "")
	m.SetModelName("opus")
	m.SetContextWindow(200000)

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 24))
	t.Cleanup(func() {
		_ = tm.Quit()
	})

	// First turn: 5000 input + 3000 output = 8000.
	tm.Send(tui.AgentEventMsg{
		SessionID: "main",
		Ev: agent.Event{
			Kind: agent.EventUsageUpdated,
			Usage: &common.Usage{
				InputTokens:  5000,
				OutputTokens: 3000,
			},
		},
	})

	// Second turn: 2000 input + 1000 output = 3000.
	// Cumulative: 7000 input + 4000 output = 11000 = "11.0k".
	tm.Send(tui.AgentEventMsg{
		SessionID: "main",
		Ev: agent.Event{
			Kind: agent.EventUsageUpdated,
			Usage: &common.Usage{
				InputTokens:  2000,
				OutputTokens: 1000,
			},
		},
	})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("11.0k"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/tui/ -run TestStatusBarCumulativeTokens -v`
Expected: FAIL -- the status bar will show "3.0k" (only second turn) instead of "11.0k".

**Step 3: Write the fix**

In `gohome/internal/tui/model_agent.go`, change lines 122-126 from:

```go
case agent.EventUsageUpdated:
    if ev.Usage != nil {
        sv.Usage = *ev.Usage
        m.checkContextWarnings(sv)
    }
```

To:

```go
case agent.EventUsageUpdated:
    if ev.Usage != nil {
        sv.Usage.InputTokens += ev.Usage.InputTokens
        sv.Usage.OutputTokens += ev.Usage.OutputTokens
        sv.Usage.CacheReadTokens += ev.Usage.CacheReadTokens
        sv.Usage.CacheWriteTokens += ev.Usage.CacheWriteTokens
        m.checkContextWarnings(sv)
    }
```

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/tui/ -run TestStatusBarCumulativeTokens -v`
Expected: PASS

**Step 5: Update snapshot golden files**

The `with_tokens_overlay` snapshot test sends a single usage event. The token values themselves have not changed (still one event with the same numbers), so the golden file should be unchanged. Verify by running:

Run: `go test ./gohome/internal/tui/ -run TestSnapshots -v`
Expected: PASS (no golden files affected since each snapshot only sends one usage event)

**Step 6: Commit**

```bash
git add gohome/internal/tui/model_agent.go gohome/internal/tui/statusbar_test.go
git commit -m "fix: accumulate token usage across turns instead of replacing"
```

---

### Task 2: Add CWD to the statusbar

**Files:**
- Modify: `gohome/internal/tui/statusbar.go:33-105` (add `shortenPath` helper and CWD segment)
- Modify: `gohome/cmd/gohome/main.go:354-355` (wire `SetCWD` and `SetHomeDir`)
- Test: `gohome/internal/tui/statusbar_test.go` (new test)

**Step 1: Write the failing test**

Add a test to `gohome/internal/tui/statusbar_test.go`:

```go
// TestStatusBarShowsCWD verifies that the status bar includes the shortened CWD.
func TestStatusBarShowsCWD(t *testing.T) {
	m := tui.New(nil, "")
	m.SetModelName("opus")
	m.SetHomeDir("/Users/testuser")
	m.SetCWD("/Users/testuser/projects/myapp")

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 24))
	t.Cleanup(func() {
		_ = tm.Quit()
	})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("~/projects/myapp"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/tui/ -run TestStatusBarShowsCWD -v`
Expected: FAIL -- CWD is not rendered in the status bar.

**Step 3: Implement the CWD display in the statusbar**

In `gohome/internal/tui/statusbar.go`, add the `strings` import and a `shortenPath` helper, then insert the CWD segment into the status bar format string.

Add `"strings"` to the import block.

Add this helper before `statusBar()`:

```go
// shortenPath replaces the home directory prefix with "~".
func shortenPath(path, home string) string {
	if home != "" && strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
```

In `statusBar()`, after the `project`/`gitBranch` block (after line 69), add the CWD segment:

```go
cwdDisplay := ""
if m.cwd != "" {
    cwdDisplay = shortenPath(m.cwd, m.homeDir)
}
```

Update the format string (line 71) from:

```go
line := fmt.Sprintf("%s · %s · %s %s/%s (%d%%)",
    project,
    modelDisplay,
    bar,
```

To:

```go
line := fmt.Sprintf("%s · %s · %s · %s %s/%s (%d%%)",
    project,
    cwdDisplay,
    modelDisplay,
    bar,
```

But only include the CWD segment when it is non-empty. To keep it clean:

```go
var segments []string
segments = append(segments, project)
if cwdDisplay != "" {
    segments = append(segments, cwdDisplay)
}
segments = append(segments, modelDisplay)
segments = append(segments, fmt.Sprintf("%s %s/%s (%d%%)", bar, formatTokens(used), formatTokens(total), pct))

line := strings.Join(segments, " · ")
```

This replaces the single `fmt.Sprintf` call at line 71-78.

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/tui/ -run TestStatusBarShowsCWD -v`
Expected: PASS

**Step 5: Wire up `SetCWD` and `SetHomeDir` in main.go**

In `gohome/cmd/gohome/main.go`, after line 355 (`m.SetProjectDir(filepath.Base(cwd))`), add:

```go
m.SetCWD(cwd)
m.SetHomeDir(userHome)
```

**Step 6: Update snapshot golden files**

The snapshot tests use `newSized()` which does not call `SetCWD`, so `cwdDisplay` will be empty and the CWD segment will be omitted. Golden files should be unaffected.

Run: `go test ./gohome/internal/tui/ -run TestSnapshots -v`
Expected: PASS (golden files unchanged)

If any golden files fail, regenerate with:

Run: `go test ./gohome/internal/tui/ -run TestSnapshots -update`

**Step 7: Commit**

```bash
git add gohome/internal/tui/statusbar.go gohome/internal/tui/statusbar_test.go gohome/cmd/gohome/main.go
git commit -m "feat: show CWD in statusbar with ~ shortening for home directory"
```

---

### Task 3: Start spinner immediately on message send

**Files:**
- Modify: `gohome/internal/tui/model_keys.go:140-151`
- Test: `gohome/internal/tui/tui_test.go` (new test)

**Step 1: Write the failing test**

Add a test to `gohome/internal/tui/tui_test.go`:

```go
// TestSpinnerStartsOnSend verifies the spinner shows "Sending..." immediately
// after submitting a message, before any streaming events arrive.
func TestSpinnerStartsOnSend(t *testing.T) {
	fe := tui.NewFrontend()
	m := tui.New(fe, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() {
		_ = tm.Quit()
	})

	// Drain the input channel so sendInputCmd does not block.
	go func() {
		for range fe.InputCh() {
		}
	}()

	// Type a message and press Enter.
	tm.Type("hello")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// The spinner should appear with "Sending..." before any streaming events.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Sending..."))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/tui/ -run TestSpinnerStartsOnSend -v`
Expected: FAIL -- "Sending..." does not appear because the spinner is not started on submit.

**Step 3: Start the spinner on message send**

In `gohome/internal/tui/model_keys.go`, in the Enter handler's `else` block (the path for a normal message submit, around line 140-151), add spinner start lines after `m.rebuildViewport()` and before `cmds = append(cmds, m.sendInputCmd(text))`:

Change from:

```go
} else {
    sv.Timeline = append(sv.Timeline, TimelineEntry{
        Kind: KindUser,
        Text: text,
    })
    sv.InFlight = true
    m.editor.SetValue("")
    m.statusMsg = ""
    m.cursor = len(sv.Timeline) - 1
    m.rebuildViewport()
    cmds = append(cmds, m.sendInputCmd(text))
}
```

To:

```go
} else {
    sv.Timeline = append(sv.Timeline, TimelineEntry{
        Kind: KindUser,
        Text: text,
    })
    sv.InFlight = true
    m.editor.SetValue("")
    m.statusMsg = ""
    m.cursor = len(sv.Timeline) - 1
    m.rebuildViewport()
    m.spinner.Start("Sending...")
    m.spinner.SetOnCancel(m.cancelFocusedSession)
    cmds = append(cmds, SpinnerTickCmd())
    cmds = append(cmds, m.sendInputCmd(text))
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/tui/ -run TestSpinnerStartsOnSend -v`
Expected: PASS

**Step 5: Run the full test suite**

Run: `go test ./gohome/internal/tui/ -v`
Expected: All tests pass. The snapshot golden files should not be affected since the snapshot tests use `newSized()` and `apply()` (synchronous, no spinner tick cmd is consumed).

**Step 6: Commit**

```bash
git add gohome/internal/tui/model_keys.go gohome/internal/tui/tui_test.go
git commit -m "feat: start spinner with 'Sending...' immediately on message submit"
```

---

### Task 4: Final verification

**Step 1: Run the full test suite**

Run: `go test ./gohome/... -v`
Expected: All tests pass.

**Step 2: Run vet and lint**

Run: `go vet ./gohome/...`
Expected: No issues.

Run: `golangci-lint run ./gohome/...`
Expected: No issues.

**Step 3: Build**

Run: `go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome`
Expected: Clean build, binary produced at `bin/gohome`.
