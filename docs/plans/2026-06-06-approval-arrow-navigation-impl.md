# Approval Prompt Arrow Key Navigation — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add Up/Down arrow key navigation to the tool call approval prompt, so users can select options with arrows + Enter in addition to the existing 1-4 number key shortcuts.

**Architecture:** Add a `selected int` field (0-3) to the `approvalPrompt` struct. Arrow keys change it, Enter dispatches the action for that index. Rendering adds a `> ` marker to the selected option. Number keys continue to work as instant shortcuts.

**Tech Stack:** Go, Bubble Tea (charmbracelet/bubbletea), lipgloss

---

### Task 1: Add `selected` field to `approvalPrompt`

**Files:**
- Modify: `gohome/internal/tui/approval.go:25-37` (struct definition)
- Modify: `gohome/internal/tui/approval.go:40-55` (constructor)

**Step 1: Add the field to the struct**

In `gohome/internal/tui/approval.go`, add `selected int` to the `approvalPrompt` struct, after the `steering` / `steerInput` fields:

```go
type approvalPrompt struct {
	req     guard.ApprovalRequest
	reply   chan guard.ApprovalDecision
	pattern string

	editing      bool
	patternInput textinput.Model

	steering   bool
	steerInput textinput.Model

	selected int // 0=Allow once, 1=Allow always, 2=Deny, 3=Deny+steer
}
```

No change needed in `newApprovalPrompt` — Go zero-initializes `selected` to `0`, which is "Allow once" (the desired default).

**Step 2: Run existing tests to confirm no breakage**

Run: `go test ./gohome/internal/tui/ -run TestApproval -v -count=1`
Expected: All existing approval tests PASS (the new field is zero-valued and unused so far).

**Step 3: Commit**

```bash
git add gohome/internal/tui/approval.go
git commit -m "feat(tui): add selected field to approvalPrompt for arrow navigation"
```

---

### Task 2: Update rendering to show selection marker

**Files:**
- Modify: `gohome/internal/tui/approval.go:83-119` (`renderApprovalOverlay` function)

**Step 1: Write failing test — selected option shows `>` marker**

Add to `gohome/internal/tui/approval_test.go`:

```go
func TestApprovalArrowRenderShowsMarker(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	msg, _ := makeApprovalReq("main", "bash", "^ls", json.RawMessage(`{"command":"ls"}`))
	tm.Send(msg)

	// Default selection is option 0 (Allow once) — should show "> [1]".
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("> [1]"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/tui/ -run TestApprovalArrowRenderShowsMarker -v -count=1`
Expected: FAIL — the current render does not include `> [1]`.

**Step 3: Update `renderApprovalOverlay` to show marker**

Replace the top-level menu rendering block (the `} else {` branch that renders `[1]`..`[4]`) in `renderApprovalOverlay` in `gohome/internal/tui/approval.go`. The function receives `ap *approvalPrompt` so it can read `ap.selected`.

Replace the block starting at the `} else {` (around line 102-109) with:

```go
	} else {
		marker := func(i int) string {
			if i == ap.selected {
				return "> "
			}
			return "  "
		}
		fmt.Fprintf(&sb, "\n%s[1] Allow once\n", marker(0))
		fmt.Fprintf(&sb, "%s[2] Allow always   pattern: %s  (e to edit)\n", marker(1), ap.pattern)
		fmt.Fprintf(&sb, "%s[3] Deny\n", marker(2))
		fmt.Fprintf(&sb, "%s[4] Deny + steer\n", marker(3))
		sb.WriteString("Esc: deny | arrows to navigate")
	}
```

Also update the editing sub-mode block (the `} else if ap.editing {` branch, around line 97-101) to use `  ` prefixes for consistency:

```go
	} else if ap.editing {
		sb.WriteString("\n  [1] Allow once\n")
		fmt.Fprintf(&sb, "  [2] Allow always   pattern: %s\n", ap.patternInput.View())
		sb.WriteString("  [3] Deny\n")
		sb.WriteString("  [4] Deny + steer\n")
		sb.WriteString("(Enter to confirm pattern, Esc to cancel edit)")
	}
```

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/tui/ -run TestApprovalArrowRenderShowsMarker -v -count=1`
Expected: PASS

**Step 5: Run all approval tests to check for regressions**

Run: `go test ./gohome/internal/tui/ -run TestApproval -v -count=1`
Expected: All PASS

**Step 6: Commit**

```bash
git add gohome/internal/tui/approval.go gohome/internal/tui/approval_test.go
git commit -m "feat(tui): render selection marker on approval prompt options"
```

---

### Task 3: Add arrow key handling

**Files:**
- Modify: `gohome/internal/tui/model.go:776-797` (`handleApprovalKey`, top-level menu section)

**Step 1: Write failing test — Down arrow changes selection**

Add to `gohome/internal/tui/approval_test.go`:

```go
func TestApprovalArrowDownChangesSelection(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	msg, _ := makeApprovalReq("main", "bash", "^ls", json.RawMessage(`{"command":"ls"}`))
	tm.Send(msg)

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("> [1]"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Press Down -> selection moves to option 1 (Allow always).
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("> [2]"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/tui/ -run TestApprovalArrowDownChangesSelection -v -count=1`
Expected: FAIL — Down key is not handled in approval menu.

**Step 3: Add arrow key cases to `handleApprovalKey`**

In `gohome/internal/tui/model.go`, in the top-level approval menu switch (the final `switch` block in `handleApprovalKey`), add cases for Up, Down, and Enter before the existing `case msg.Type == tea.KeyEsc:`:

```go
	switch {
	case msg.Type == tea.KeyUp:
		if ap.selected > 0 {
			ap.selected--
		}
	case msg.Type == tea.KeyDown:
		if ap.selected < 3 {
			ap.selected++
		}
	case msg.Type == tea.KeyEnter:
		switch ap.selected {
		case 0:
			m.resolveApproval(guard.ApprovalDecision{Outcome: guard.AllowOnce})
		case 1:
			m.resolveApproval(guard.ApprovalDecision{
				Outcome:      guard.AllowAlways,
				SavedPattern: ap.pattern,
			})
		case 2:
			m.resolveApproval(guard.ApprovalDecision{Outcome: guard.Deny})
		case 3:
			ap.steering = true
			ap.steerInput.Focus()
		}
	case msg.Type == tea.KeyEsc:
		// ... existing code unchanged ...
	}
```

**Step 4: Run test to verify it passes**

Run: `go test ./gohome/internal/tui/ -run TestApprovalArrowDownChangesSelection -v -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/tui/model.go gohome/internal/tui/approval_test.go
git commit -m "feat(tui): add arrow key navigation to approval prompt"
```

---

### Task 4: Add Enter-to-confirm tests for each option

**Files:**
- Modify: `gohome/internal/tui/approval_test.go`

**Step 1: Write test — Enter on default selection dispatches AllowOnce**

```go
func TestApprovalEnterDispatchesAllowOnce(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	msg, ch := makeApprovalReq("main", "bash", "^ls", json.RawMessage(`{"command":"ls"}`))
	tm.Send(msg)

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("> [1]"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Enter on default (option 0) -> AllowOnce.
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case dec := <-ch:
		if dec.Outcome != guard.AllowOnce {
			t.Fatalf("expected AllowOnce, got %q", dec.Outcome)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}
```

**Step 2: Write test — Down+Down+Enter dispatches Deny**

```go
func TestApprovalArrowDownDownEnterDenies(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	msg, ch := makeApprovalReq("main", "bash", "^ls", json.RawMessage(`{"command":"ls"}`))
	tm.Send(msg)

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("> [1]"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Down twice -> option 2 (Deny), then Enter.
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case dec := <-ch:
		if dec.Outcome != guard.Deny {
			t.Fatalf("expected Deny, got %q", dec.Outcome)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}
```

**Step 3: Write test — Up arrow clamps at 0**

```go
func TestApprovalArrowUpClampsAtZero(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	msg, _ := makeApprovalReq("main", "bash", "^ls", json.RawMessage(`{"command":"ls"}`))
	tm.Send(msg)

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("> [1]"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Press Up at top -> should stay on option 0.
	tm.Send(tea.KeyMsg{Type: tea.KeyUp})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("> [1]"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
```

**Step 4: Write test — Down+Enter on option 1 dispatches AllowAlways with pattern**

```go
func TestApprovalArrowDownEnterAllowAlways(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	msg, ch := makeApprovalReq("main", "bash", "^ls", json.RawMessage(`{"command":"ls"}`))
	tm.Send(msg)

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("> [1]"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Down once -> option 1 (Allow always), then Enter.
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case dec := <-ch:
		if dec.Outcome != guard.AllowAlways {
			t.Fatalf("expected AllowAlways, got %q", dec.Outcome)
		}
		if dec.SavedPattern != "^ls" {
			t.Fatalf("expected pattern %q, got %q", "^ls", dec.SavedPattern)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}
```

**Step 5: Write test — Arrow to Deny+steer enters steer mode**

```go
func TestApprovalArrowToSteerEntersSteerMode(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	msg, ch := makeApprovalReq("main", "bash", "^ls", json.RawMessage(`{"command":"ls"}`))
	tm.Send(msg)

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("> [1]"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Down 3 times -> option 3 (Deny+steer), then Enter to enter steer mode.
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Should show steer input prompt.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Steer message"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Type a message and confirm.
	tm.Type("try another approach")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case dec := <-ch:
		if dec.Outcome != guard.DenySteer {
			t.Fatalf("expected DenySteer, got %q", dec.Outcome)
		}
		if dec.SteerMessage != "try another approach" {
			t.Fatalf("expected steer msg %q, got %q", "try another approach", dec.SteerMessage)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}
```

**Step 6: Run all tests**

Run: `go test ./gohome/internal/tui/ -run TestApproval -v -count=1`
Expected: All PASS (including the new tests and all existing ones)

**Step 7: Commit**

```bash
git add gohome/internal/tui/approval_test.go
git commit -m "test(tui): add arrow navigation and Enter-to-confirm approval tests"
```

---

### Task 5: Run full test suite and verify

**Files:** None (verification only)

**Step 1: Run all TUI tests**

Run: `go test ./gohome/internal/tui/ -v -count=1`
Expected: All PASS, no regressions

**Step 2: Run full project tests**

Run: `go test ./... -count=1`
Expected: All PASS
