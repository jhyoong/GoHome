# Help Overlay (Ctrl+H) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a help box overlay triggered by Ctrl+H that lists all keyboard shortcuts, slash commands, and CLI flags.

**Architecture:** Follows the existing `/tokens` overlay pattern — a `showHelp` bool on Model, a `renderHelpOverlay()` method in a new `help.go` file, Esc to close. When active, it replaces the chat area and blocks other key input.

**Tech Stack:** Go, Bubble Tea (bubbletea), lipgloss, teatest + golden files for testing.

---

### Task 1: Add `showHelp` field and key handlers to Model

**Files:**
- Modify: `gohome/internal/tui/model.go:103` (add field after `showTokens`)
- Modify: `gohome/internal/tui/model.go:587-593` (add help overlay Esc handler)
- Modify: `gohome/internal/tui/model.go:611` (add Ctrl+H case in key switch)
- Modify: `gohome/internal/tui/model.go:1119-1123` (add help overlay rendering in View)

**Step 1: Add `showHelp` field to Model struct**

In `model.go`, after line 103 (`showTokens bool`), add:

```go
	showHelp   bool
```

**Step 2: Add help overlay Esc handler in Update()**

In `model.go`, after the `showTokens` Esc block (lines 587-593), add:

```go
		if m.showHelp {
			if msg.Type == tea.KeyEsc {
				m.showHelp = false
			}
			return m, tea.Batch(cmds...)
		}
```

**Step 3: Add Ctrl+H key handler**

In `model.go`, in the `switch msg.Type` block (after line 611), add a new case:

```go
		case tea.KeyCtrlH:
			m.showHelp = true
			return m, tea.Batch(cmds...)
```

This should be placed right after the existing `tea.KeyCtrlCloseBracket` / `tea.KeyCtrlOpenBracket` cases (around line 614-615).

**Step 4: Add help overlay rendering in View()**

In `model.go`, after the `showTokens` rendering block (lines 1119-1123), add:

```go
	if m.showHelp {
		sections = append(sections, m.renderHelpOverlay())
		sections = append(sections, m.statusBar())
		return strings.Join(sections, "\n")
	}
```

**Step 5: Add exported accessors for tests**

At the bottom of `model.go`, after the `OpenTokensOverlay` method (line 1259), add:

```go
func (m *Model) ShowHelp() bool {
	return m.showHelp
}

func (m *Model) OpenHelpOverlay() {
	m.showHelp = true
}
```

**Step 6: Commit**

```bash
git add gohome/internal/tui/model.go
git commit -m "feat(tui): add showHelp field and Ctrl+H/Esc key handlers"
```

---

### Task 2: Create `help.go` with `renderHelpOverlay()`

**Files:**
- Create: `gohome/internal/tui/help.go`

**Step 1: Create `help.go`**

```go
package tui

import "strings"

func (m *Model) renderHelpOverlay() string {
	var sb strings.Builder
	sb.WriteString("Keyboard shortcuts\n")
	sb.WriteString("  Ctrl+C        Cancel turn / double-tap to quit\n")
	sb.WriteString("  Ctrl+E        Open external editor\n")
	sb.WriteString("  Ctrl+H        Toggle this help\n")
	sb.WriteString("  Ctrl+]        Focus next session\n")
	sb.WriteString("  Ctrl+[        Focus prev session\n")
	sb.WriteString("  PgUp/PgDown   Scroll chat\n")
	sb.WriteString("  Enter         Submit input / toggle tool detail\n")
	sb.WriteString("  Alt+Enter     Insert newline\n")
	sb.WriteString("  Tab           Autocomplete / confirm file search\n")
	sb.WriteString("  Esc           Close overlay / cancel\n")
	sb.WriteString("  @             File search\n")
	sb.WriteString("\n")
	sb.WriteString("Slash commands\n")
	sb.WriteString("  /new          Start a new session\n")
	sb.WriteString("  /resume       Resume a past session\n")
	sb.WriteString("  /yolo         Toggle YOLO mode (skip approvals)\n")
	sb.WriteString("  /endpoint     Switch endpoint\n")
	sb.WriteString("  /model        Switch model\n")
	sb.WriteString("  /cancel       Cancel current turn\n")
	sb.WriteString("  /tokens       Show token usage\n")
	sb.WriteString("  /quit         Quit gohome\n")
	sb.WriteString("\n")
	sb.WriteString("CLI flags\n")
	sb.WriteString("  --endpoint    Endpoint name override\n")
	sb.WriteString("  --model       Model override\n")
	sb.WriteString("  --yolo        Disable all approval prompts\n")
	sb.WriteString("  --resume      Resume most recent session\n")
	sb.WriteString("  --version     Print version and exit\n")
	sb.WriteString("\n")
	sb.WriteString("Esc to close")
	return sb.String()
}
```

**Step 2: Verify it compiles**

Run: `cd gohome && go build ./...`
Expected: No errors.

**Step 3: Commit**

```bash
git add gohome/internal/tui/help.go
git commit -m "feat(tui): add renderHelpOverlay with shortcuts, commands, and flags"
```

---

### Task 3: Add tests

**Files:**
- Create: `gohome/internal/tui/help_test.go`

**Step 1: Write tests**

```go
package tui_test

import (
	"bytes"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/jhyoong/GoHome/gohome/internal/tui"
)

func TestHelpOverlay_CtrlH_Opens(t *testing.T) {
	m := tui.New(nil, "")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() {
		_ = tm.Quit()
	})

	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlH})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Keyboard shortcuts")) &&
			bytes.Contains(out, []byte("Slash commands")) &&
			bytes.Contains(out, []byte("CLI flags"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

func TestHelpOverlay_Esc_Closes(t *testing.T) {
	m := newSized()
	m.OpenHelpOverlay()

	if !m.ShowHelp() {
		t.Fatal("expected ShowHelp to be true after OpenHelpOverlay")
	}

	m = apply(m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.ShowHelp() {
		t.Fatal("expected ShowHelp to be false after Esc")
	}
}

func TestHelpOverlay_BlocksOtherKeys(t *testing.T) {
	m := newSized()
	m.OpenHelpOverlay()

	m = apply(m, tea.KeyMsg{Type: tea.KeyPgUp})
	m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})

	if !m.ShowHelp() {
		t.Fatal("expected help overlay to remain open after non-Esc keys")
	}
}
```

Note: `newSized` and `apply` are already defined in `tui_snapshot_test.go` in the same `tui_test` package, so they are available here.

**Step 2: Run the tests**

Run: `cd gohome && go test ./internal/tui/ -run TestHelpOverlay -v`
Expected: All 3 tests PASS.

**Step 3: Add snapshot test**

Add a new subtest to `TestSnapshots` in `tui_snapshot_test.go`:

```go
	t.Run("with_help_overlay", func(t *testing.T) {
		m := newSized()
		m.OpenHelpOverlay()
		golden.RequireEqual(t, []byte(m.View()))
	})
```

**Step 4: Generate the golden file**

Run: `cd gohome && go test ./internal/tui/ -run TestSnapshots/with_help_overlay -update`
Expected: Golden file created at `gohome/internal/tui/testdata/TestSnapshots/with_help_overlay.golden`.

**Step 5: Run full test suite to check for regressions**

Run: `cd gohome && go test ./...`
Expected: All tests PASS.

**Step 6: Commit**

```bash
git add gohome/internal/tui/help_test.go gohome/internal/tui/tui_snapshot_test.go gohome/internal/tui/testdata/
git commit -m "test(tui): add help overlay tests and snapshot"
```

---

### Task 4: Add `/help` slash command alias

**Files:**
- Modify: `gohome/internal/tui/model.go:888` (add to `slashCommands` list)
- Modify: `gohome/internal/tui/model.go:905` (add case in `handleSlashCommand`)

**Step 1: Add `/help` to the slash commands list**

In `model.go` line 888, update:

```go
var slashCommands = []string{
	"/help", "/new", "/resume", "/yolo", "/endpoint", "/model", "/cancel", "/tokens", "/quit",
}
```

**Step 2: Add `/help` case in `handleSlashCommand`**

In `model.go`, in the `handleSlashCommand` switch, add before the `default` case:

```go
	case "/help":
		m.showHelp = true
		m.statusMsg = ""
```

**Step 3: Update the help overlay content to include `/help`**

In `help.go`, add this line in the Slash commands section (after the `/new` line):

```go
	sb.WriteString("  /help         Show this help\n")
```

**Step 4: Run tests**

Run: `cd gohome && go test ./internal/tui/ -run TestSnapshots/with_help_overlay -update && go test ./...`
Expected: All tests PASS (golden file updated with new `/help` line).

**Step 5: Commit**

```bash
git add gohome/internal/tui/model.go gohome/internal/tui/help.go gohome/internal/tui/testdata/
git commit -m "feat(tui): add /help slash command alias for help overlay"
```
