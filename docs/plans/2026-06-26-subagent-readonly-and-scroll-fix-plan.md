# Subagent Read-Only Mode & Scroll Fix Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix two TUI issues: (1) disable text input on completed subagent sessions, (2) fix WrapText to handle newlines so scroll math is accurate.

**Architecture:** Issue 1 adds an `EndReason` field to `Event`, propagates it through `Spawn` to the TUI, and gates input on a new `Completed` flag on `SessionView`. Issue 2 refactors `WrapText` to split on newlines before wrapping, fixing scroll line count mismatches. Both issues are independent and can be implemented in either order.

**Tech Stack:** Go, Bubble Tea (charmbracelet/bubbletea), golden-file snapshot tests (charmbracelet/x/exp/golden)

---

## Task 1: Add EndReason to Event struct

**Files:**
- Modify: `gohome/internal/agent/events.go:36-48`
- Test: `gohome/internal/agent/events_test.go`

**Step 1: Add the field**

In `gohome/internal/agent/events.go`, add `EndReason` to the `Event` struct after `StopReason`:

```go
type Event struct {
	Kind          EventKind
	SessionID     string
	TextDelta     string
	ToolCallID    string
	ToolName      string
	InputJSON     string
	Result        *ToolResult
	Usage         *common.Usage
	StopReason    string
	EndReason     string // "done" or "cancelled" (EventSessionEnded only)
	Err           error
	ThinkingDelta string
}
```

**Step 2: Run vet**

Run: `go vet ./gohome/internal/agent/...`
Expected: PASS (no uses of EndReason yet, so no breakage)

**Step 3: Commit**

```
feat(agent): add EndReason field to Event struct
```

---

## Task 2: Populate EndReason in Spawn

**Files:**
- Modify: `gohome/internal/agent/spawn.go:106-122`
- Test: `gohome/internal/agent/spawn_test.go`

**Step 1: Write the failing test**

In `gohome/internal/agent/spawn_test.go`, add a test that verifies `EventSessionEnded` carries `EndReason: "done"` on normal completion:

```go
func TestSpawn_EndReasonDone(t *testing.T) {
	home := t.TempDir()
	client := oneTextTurnClient("answer")
	a := buildSpawnParent(t, client, home)
	fe := a.Frontend.(*fakeRecorder)

	_, _, err := a.Spawn(context.Background(), "task", "")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	for _, ev := range fe.events {
		if ev.Kind == EventSessionEnded {
			if ev.EndReason != "done" {
				t.Errorf("EndReason = %q, want %q", ev.EndReason, "done")
			}
			return
		}
	}
	t.Fatal("no EventSessionEnded found")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/agent/ -run TestSpawn_EndReasonDone -v`
Expected: FAIL -- `EndReason` is `""`, not `"done"`

**Step 3: Write the cancelled test**

Add a test that verifies `EndReason: "cancelled"` when context is cancelled:

```go
func TestSpawn_EndReasonCancelled(t *testing.T) {
	home := t.TempDir()
	bgCtx, bgCancel := context.WithCancel(context.Background())
	t.Cleanup(bgCancel)
	client := &blockingClient{firstDelta: "partial", bgCtx: bgCtx}

	wl, err := guardCompileEmpty(t)
	if err != nil {
		t.Fatalf("guard compile: %v", err)
	}
	g := guardNewYolo(wl)
	fe := &fakeRecorder{}

	parentSess := session.NewSession("parent", home, "model", "anthropic")
	a := &Agent{
		Tools:    tools.NewRegistry(),
		Guard:    g,
		Frontend: fe,
		State:    NewSessionState(parentSess, nil, client),
		System:   "system",
		Home:     home,
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, _, _ = a.Spawn(ctx, "task", "")

	for _, ev := range fe.events {
		if ev.Kind == EventSessionEnded && ev.SessionID == "sub-1" {
			if ev.EndReason != "cancelled" {
				t.Errorf("EndReason = %q, want %q", ev.EndReason, "cancelled")
			}
			return
		}
	}
	t.Fatal("no EventSessionEnded found for sub-1")
}
```

**Step 4: Run cancelled test to verify it fails**

Run: `go test ./gohome/internal/agent/ -run TestSpawn_EndReasonCancelled -v`
Expected: FAIL

**Step 5: Implement -- move endReason computation before emit**

In `gohome/internal/agent/spawn.go`, restructure lines 107-122. Move the `endReason` computation before the `Frontend.Emit` call:

```go
// Run the child agent.
runErr := childAgent.Run(ctx, child)

// Determine end reason before emitting.
endReason := "done"
if errors.Is(runErr, context.Canceled) || ctx.Err() != nil {
	endReason = "cancelled"
}

a.Frontend.Emit(childID, Event{
	Kind:      EventSessionEnded,
	SessionID: childID,
	EndReason: endReason,
})

// Determine whether the run ended in error.
isError := runErr != nil

// Emit session_end on the child writer.
cw.Emit(session.SessionEnd{Reason: endReason})
```

**Step 6: Run both tests to verify they pass**

Run: `go test ./gohome/internal/agent/ -run "TestSpawn_EndReason" -v`
Expected: PASS

**Step 7: Run full agent test suite**

Run: `go test ./gohome/internal/agent/ -v`
Expected: PASS (no regressions)

**Step 8: Commit**

```
feat(agent): populate EndReason on EventSessionEnded in Spawn
```

---

## Task 3: Add Completed field to SessionView and wire it up

**Files:**
- Modify: `gohome/internal/tui/model.go:52-63`
- Modify: `gohome/internal/tui/model_agent.go:148-149`
- Test: `gohome/internal/tui/tui_snapshot_test.go`

**Step 1: Write the failing test**

In `gohome/internal/tui/tui_snapshot_test.go`, add a test that checks `Completed` is set after `EventSessionEnded` with `EndReason: "done"`:

```go
func TestSubagentCompleted_OnDone(t *testing.T) {
	m := newSized()
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventSessionStarted,
		SessionID: "sub-1",
	}})
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventSessionEnded,
		SessionID: "sub-1",
		EndReason: "done",
	}})
	sv := m.Sessions()["sub-1"]
	if !sv.Completed {
		t.Fatal("expected Completed=true after EventSessionEnded with EndReason='done'")
	}
}

func TestSubagentNotCompleted_OnCancel(t *testing.T) {
	m := newSized()
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventSessionStarted,
		SessionID: "sub-1",
	}})
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventSessionEnded,
		SessionID: "sub-1",
		EndReason: "cancelled",
	}})
	sv := m.Sessions()["sub-1"]
	if sv.Completed {
		t.Fatal("expected Completed=false after EventSessionEnded with EndReason='cancelled'")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/tui/ -run "TestSubagent(Not)?Completed" -v`
Expected: FAIL -- `Completed` field does not exist

**Step 3: Add the field and wire it**

In `gohome/internal/tui/model.go`, add `Completed bool` to `SessionView`:

```go
type SessionView struct {
	ID        string
	Depth     int
	Title     string
	Timeline  []TimelineEntry
	InFlight  bool
	Completed bool
	Usage     common.Usage

	warned80 bool
	warned95 bool
}
```

In `gohome/internal/tui/model_agent.go`, update the `EventSessionEnded` case:

```go
case agent.EventSessionEnded:
	sv.InFlight = false
	if ev.EndReason == "done" {
		sv.Completed = true
	}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/tui/ -run "TestSubagent(Not)?Completed" -v`
Expected: PASS

**Step 5: Commit**

```
feat(tui): set Completed flag on subagent session done
```

---

## Task 4: Gate text input on Completed sessions

**Files:**
- Modify: `gohome/internal/tui/model_keys.go:118-139`
- Test: `gohome/internal/tui/tui_snapshot_test.go`

**Step 1: Write the failing test**

In `gohome/internal/tui/tui_snapshot_test.go`, add a test that verifies typing in a completed session is rejected:

```go
func TestCompletedSession_RejectsInput(t *testing.T) {
	m := newSized()
	// Create and complete a subagent session.
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventSessionStarted,
		SessionID: "sub-1",
	}})
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventSessionEnded,
		SessionID: "sub-1",
		EndReason: "done",
	}})
	// Focus the completed session.
	m = apply(m, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})

	// Type something into the editor.
	for _, r := range "hello" {
		m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})

	// Should show "Session complete" status, not add a user entry.
	if m.StatusMsg() != "Session complete" {
		t.Errorf("StatusMsg = %q, want %q", m.StatusMsg(), "Session complete")
	}
	sv := m.Sessions()["sub-1"]
	for _, e := range sv.Timeline {
		if e.Kind == tui.KindUser {
			t.Fatal("user entry should not be added to completed session")
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/tui/ -run TestCompletedSession_RejectsInput -v`
Expected: FAIL -- input is accepted

**Step 3: Add input gating**

In `gohome/internal/tui/model_keys.go`, in the `tea.KeyEnter` handler, after the `text != ""` check (line 118 area), add a `Completed` check before the `InFlight` check:

```go
} else {
	sv := m.getOrCreateSession(m.focused, 0)
	if sv.Completed {
		m.statusMsg = "Session complete"
		m.editor.SetValue("")
	} else if sv.InFlight {
		if len(m.pendingMessages) >= 10 {
			m.statusMsg = "Message queue full (10)"
		} else {
			m.pendingMessages = append(m.pendingMessages, text)
			m.editor.SetValue("")
		}
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
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/tui/ -run TestCompletedSession_RejectsInput -v`
Expected: PASS

**Step 5: Verify navigation still works on completed sessions**

Add a test confirming arrow keys and expand/collapse still work:

```go
func TestCompletedSession_AllowsNavigation(t *testing.T) {
	m := newSized()
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventSessionStarted,
		SessionID: "sub-1",
	}})
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventTokenDelta,
		SessionID: "sub-1",
		TextDelta: "Hello from subagent",
	}})
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventSessionEnded,
		SessionID: "sub-1",
		EndReason: "done",
	}})
	// Focus the completed session.
	m = apply(m, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})

	// Arrow down should work (not panic or be blocked).
	m = apply(m, tea.KeyMsg{Type: tea.KeyDown})
	// No crash = pass.
}
```

**Step 6: Run all new tests**

Run: `go test ./gohome/internal/tui/ -run "TestCompletedSession" -v`
Expected: PASS

**Step 7: Commit**

```
feat(tui): gate text input on completed subagent sessions
```

---

## Task 5: Render "[Session complete]" label for completed sessions

**Files:**
- Modify: `gohome/internal/tui/model.go:517-531` (View method, input region)
- Test: `gohome/internal/tui/tui_snapshot_test.go`

**Step 1: Write the snapshot test**

In `gohome/internal/tui/tui_snapshot_test.go`, add a snapshot for the completed session view:

```go
t.Run("completed_subagent_view", func(t *testing.T) {
	m := newSized()
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventSessionStarted,
		SessionID: "sub-1",
	}})
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventTokenDelta,
		SessionID: "sub-1",
		TextDelta: "Subagent result text.",
	}})
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventTurnDone,
		SessionID: "sub-1",
	}})
	m = apply(m, tui.AgentEventMsg{SessionID: "sub-1", Ev: agent.Event{
		Kind:      agent.EventSessionEnded,
		SessionID: "sub-1",
		EndReason: "done",
	}})
	// Focus the completed session.
	m = apply(m, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	golden.RequireEqual(t, []byte(m.View()))
})
```

**Step 2: Implement the view change**

In `gohome/internal/tui/model.go`, in the `View()` method, inside the input region rendering block (around line 523 where `else` renders the editor), add a check for completed sessions:

```go
} else {
	sv, svOK := m.sessions[m.focused]
	if svOK && sv.Completed {
		completedLabel := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("[Session complete]")
		sections = append(sections, completedLabel)
	} else {
		palette := m.slashPalette()
		if palette != "" {
			sections = append(sections, palette)
		}
		m.editor.SetTermHeight(m.winH)
		editorLines := m.editor.Render(m.winW)
		sections = append(sections, strings.Join(editorLines, "\n"))
	}
}
```

Note: `lipgloss` is already imported in `model.go` via `style.Theme`. Check if `lipgloss` needs to be added to the import -- look for existing lipgloss usage in the file. If it's not imported, add `"github.com/charmbracelet/lipgloss"` to the import block.

**Step 3: Generate the golden file**

Run: `go test ./gohome/internal/tui/ -run TestSnapshots/completed_subagent_view -update`
Expected: Creates new golden file

**Step 4: Verify all snapshot tests pass**

Run: `go test ./gohome/internal/tui/ -run TestSnapshots -v`
Expected: PASS

**Step 5: Commit**

```
feat(tui): render [Session complete] label for completed sessions
```

---

## Task 6: Fix WrapText to handle newlines

**Files:**
- Modify: `gohome/internal/tui/ansi.go:88-242`
- Test: `gohome/internal/tui/ansi_test.go`

**Step 1: Write the failing tests**

In `gohome/internal/tui/ansi_test.go`, add test cases to the existing `TestWrapText` table:

```go
{"single newline", "hello\nworld", 80, []string{"hello", "world"}},
{"newline mid wrap", "aaa bbb\nccc ddd", 7, []string{"aaa bbb", "ccc ddd"}},
{"multiple newlines", "a\n\nb", 80, []string{"a", "", "b"}},
{"trailing newline", "hello\n", 80, []string{"hello", ""}},
{"newline with wrapping", "hello world\nfoo bar baz", 11, []string{"hello world", "foo bar baz"}},
{"ansi across newlines", "\x1b[31mhello\nworld\x1b[0m", 80, []string{"\x1b[31mhello\x1b[0m", "\x1b[31mworld\x1b[0m"}},
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/tui/ -run TestWrapText -v`
Expected: FAIL -- newlines are not handled

**Step 3: Refactor WrapText**

In `gohome/internal/tui/ansi.go`, refactor `WrapText` into two functions:

1. Rename the current `WrapText` body to `wrapSingleLine(s string, maxWidth int, initialSGR string) (lines []string, finalSGR string)`. This is the same logic but:
   - Accepts an `initialSGR` parameter to set the starting `activeSGR` state
   - Returns the final `activeSGR` state alongside the wrapped lines

2. Rewrite `WrapText` as a loop over newline-split segments:

```go
func WrapText(s string, maxWidth int) []string {
	if s == "" {
		return []string{""}
	}

	segments := strings.Split(s, "\n")
	var all []string
	activeSGR := ""
	for _, seg := range segments {
		lines, nextSGR := wrapSingleLine(seg, maxWidth, activeSGR)
		all = append(all, lines...)
		activeSGR = nextSGR
	}
	return all
}
```

For `wrapSingleLine`, the changes from the current body are:
- Accept `initialSGR string` parameter, use it to initialize `activeSGR` instead of `""`
- Return `([]string, string)` where the second value is the final `activeSGR`
- Handle empty segment: if `seg == ""`, return `[]string{""}` with the current SGR carried through

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/tui/ -run TestWrapText -v`
Expected: PASS (all existing and new cases)

**Step 5: Run full TUI test suite**

Run: `go test ./gohome/internal/tui/ -v`
Expected: PASS or golden file mismatches (expected)

**Step 6: Update golden files if needed**

Run: `go test ./gohome/internal/tui/ -run TestSnapshots -update`
Then: `go test ./gohome/internal/tui/ -run TestSnapshots -v`
Expected: PASS

**Step 7: Commit**

```
fix(tui): handle newlines in WrapText for accurate scroll math
```

---

## Task 7: Fix PgUp/PgDn scroll amount

**Files:**
- Modify: `gohome/internal/tui/model_keys.go:82-85`
- Modify: `gohome/internal/tui/chat.go:17` (export `maxHeight` or add getter)

**Step 1: Check field access**

`ChatComponent.maxHeight` is lowercase (unexported). Since `model_keys.go` is in the same package (`tui`), it can access `m.chat.maxHeight` directly. No getter needed.

**Step 2: Make the change**

In `gohome/internal/tui/model_keys.go`, replace:

```go
case tea.KeyPgUp:
	m.chat.ScrollUp(5)
case tea.KeyPgDown:
	m.chat.ScrollDown(5)
```

With:

```go
case tea.KeyPgUp:
	scrollAmt := m.chat.maxHeight - 2
	if scrollAmt < 1 {
		scrollAmt = 1
	}
	m.chat.ScrollUp(scrollAmt)
case tea.KeyPgDown:
	scrollAmt := m.chat.maxHeight - 2
	if scrollAmt < 1 {
		scrollAmt = 1
	}
	m.chat.ScrollDown(scrollAmt)
```

**Step 3: Run vet and tests**

Run: `go vet ./gohome/internal/tui/... && go test ./gohome/internal/tui/ -v`
Expected: PASS

**Step 4: Commit**

```
fix(tui): use full-page scroll for PgUp/PgDn instead of 5 lines
```

---

## Task 8: Final verification

**Step 1: Run full test suite**

Run: `go test ./gohome/...`
Expected: PASS

**Step 2: Run lint**

Run: `golangci-lint run ./gohome/...`
Expected: PASS (or only pre-existing warnings)

**Step 3: Build**

Run: `go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome`
Expected: Build succeeds, binary under 25 MB
