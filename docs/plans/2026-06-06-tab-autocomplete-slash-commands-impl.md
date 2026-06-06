# Tab Autocomplete for Slash Commands — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add Tab-key completion for slash commands so pressing Tab fills in the first matching command with a trailing space, and the palette highlights which command will be selected.

**Architecture:** Add a `completeSlash()` method on `Model` that checks the editor for a `/`-prefix, runs `slashComplete`, and fills the first match. Update the `KeyTab` handler to try slash completion before file search. Update `slashPalette` to bold the first match.

**Tech Stack:** Go, Bubble Tea (charmbracelet/bubbletea), lipgloss for styling

---

### Task 1: Add `completeSlash` method and wire Tab handler

**Files:**
- Modify: `gohome/internal/tui/model.go:614-617` (KeyTab case)
- Modify: `gohome/internal/tui/model.go` (add `completeSlash` method near `slashPalette` at line ~1055)
- Test: `gohome/internal/tui/slash_test.go`

**Step 1: Write the failing tests**

Add to `gohome/internal/tui/slash_test.go`:

```go
func TestTabCompletesSlashCommand(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Type("/mo")
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})

	// After Tab, editor should show "/model " and the palette should reflect it.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("/model"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

func TestTabNoMatchDoesNothing(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Type("/xyz")
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})

	// Editor should still show "/xyz" — no completion happened.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("/xyz"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

func TestTabAlreadyCompleteDoesNothing(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Type a complete command followed by a space.
	tm.Type("/model ")
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})

	// Editor should still show "/model " — no change.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("/model"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

func TestTabCompletesFirstMatchFromSlash(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// "/" matches all commands; first in list is "/new"
	tm.Type("/")
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("/new"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./gohome/internal/tui/ -run "TestTabCompletes|TestTabNoMatch|TestTabAlreadyComplete" -v -count=1`
Expected: `TestTabCompletesSlashCommand` and `TestTabCompletesFirstMatchFromSlash` FAIL (Tab doesn't change editor content for slash). The other two may pass since they assert the original text remains.

**Step 3: Add `completeSlash` method**

Add to `gohome/internal/tui/model.go`, directly after the `slashPalette` method (after line 1055):

```go
// completeSlash fills the editor with the first matching slash command + space.
// Returns true if a completion was applied.
func (m *Model) completeSlash() bool {
	val := m.editor.Value()
	if !strings.HasPrefix(val, "/") {
		return false
	}
	matches := slashComplete(val)
	if len(matches) == 0 {
		return false
	}
	m.editor.SetValue(matches[0] + " ")
	return true
}
```

**Step 4: Wire `completeSlash` into the KeyTab handler**

In `gohome/internal/tui/model.go`, replace lines 614-617:

```go
		case tea.KeyTab:
			if m.completeSlash() {
				return m, tea.Batch(cmds...)
			}
			if m.confirmFileSearch() {
				return m, tea.Batch(cmds...)
			}
```

**Step 5: Run the tests to verify they pass**

Run: `go test ./gohome/internal/tui/ -run "TestTabCompletes|TestTabNoMatch|TestTabAlreadyComplete" -v -count=1`
Expected: All PASS

**Step 6: Commit**

```bash
git add gohome/internal/tui/model.go gohome/internal/tui/slash_test.go
git commit -m "feat(tui): add Tab completion for slash commands"
```

---

### Task 2: Highlight first match in slash palette

**Files:**
- Modify: `gohome/internal/tui/model.go:1043-1055` (`slashPalette` method)
- Test: `gohome/internal/tui/slash_test.go`

**Step 1: Write the failing test**

Add to `gohome/internal/tui/slash_test.go`:

```go
func TestSlashPaletteHighlightsFirstMatch(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("─"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Type "/re" to narrow to "/resume".
	tm.Type("/re")

	// The palette should render /resume with bold ANSI (ESC[1m).
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("\x1b[1m")) && bytes.Contains(out, []byte("/resume"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
```

**Step 2: Run the test to verify it fails**

Run: `go test ./gohome/internal/tui/ -run TestSlashPaletteHighlightsFirstMatch -v -count=1`
Expected: FAIL — palette currently renders plain text without bold ANSI.

**Step 3: Update `slashPalette` to bold the first match**

Replace the `slashPalette` method in `gohome/internal/tui/model.go` (lines 1043-1055):

```go
var slashHighlight = lipgloss.NewStyle().Bold(true)

// slashPalette renders the autocomplete list when input starts with '/'.
// The first match is highlighted (bold) to indicate Tab will select it.
// Returns "" when not applicable.
func (m *Model) slashPalette() string {
	val := m.editor.Value()
	if !strings.HasPrefix(val, "/") {
		return ""
	}
	matches := slashComplete(val)
	if len(matches) == 0 {
		return ""
	}
	parts := make([]string, len(matches))
	parts[0] = slashHighlight.Render(matches[0])
	for i := 1; i < len(matches); i++ {
		parts[i] = matches[i]
	}
	return strings.Join(parts, "  ")
}
```

**Step 4: Run the test to verify it passes**

Run: `go test ./gohome/internal/tui/ -run TestSlashPaletteHighlightsFirstMatch -v -count=1`
Expected: PASS

**Step 5: Run full test suite to check for regressions**

Run: `go test ./gohome/internal/tui/ -v -count=1`
Expected: All tests PASS

**Step 6: Commit**

```bash
git add gohome/internal/tui/model.go gohome/internal/tui/slash_test.go
git commit -m "feat(tui): highlight first match in slash palette"
```
