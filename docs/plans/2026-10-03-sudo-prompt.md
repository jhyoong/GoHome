# Sudo Password Prompt Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the sudo password prompt reliable and obvious: a two-step approval flow, a prominent password dialog, opt-in password caching, and a layout that never overflows the terminal.

**Architecture:** All UI changes live in `gohome/internal/tui`. `approvalPrompt` gains a `sudoStage` flag; while set, every key goes to the password field. A new `cacheSudoPassword` setting (read from `m.settings`, which `main.go` already passes in via `SetSettings`) gates the in-memory cache. Chat height is capped by the real height of the other sections.

**Tech Stack:** Go 1.25, Bubble Tea, bubbles/textinput, lipgloss, charmbracelet/x/exp/golden.

**Design doc:** `docs/plans/2026-10-03-sudo-prompt-design.md`

**Branch:** `feat/sudo-password-prompt` (already created and checked out).

---

## Background for the implementer

- Approval state: `gohome/internal/tui/approval.go` (struct + rendering) and `gohome/internal/tui/model_approval.go` (key handling, resolve, queue).
- Key dispatch: `model_keys.go:handleKeyMsg` sends every key to `handleApprovalKey` when `m.activeApproval != nil`. Ctrl+C is handled before that and denies.
- `resolveApproval` sends the decision on the buffered reply channel and promotes the next queued approval.
- Timeline notices: append `TimelineEntry{Kind: KindNotice, Text: ...}` to a `SessionView.Timeline`, or call `m.AddTimelineEntry(sessionID, entry)`.
- Tests: new tests go in an **internal** test file (`package tui`) so they can drive `Model.Update` synchronously and read private fields. Do not use `teatest` for new tests.
- Run tests from the repo root: `go test ./gohome/internal/tui/ -run <Name>`.
- Bool settings merge rule (existing convention, see `AutoCompact`): project can only turn a bool **on**.

---

### Task 1: Add `cacheSudoPassword` setting

**Files:**
- Modify: `gohome/internal/config/config.go` (Settings struct ~line 53, merge ~line 95 and ~line 135, sources ~line 328)
- Modify: `gohome/internal/config/skeleton.go:40`
- Test: `gohome/internal/config/config_test.go`, `gohome/internal/config/skeleton_test.go`

**Step 1: Write the failing tests**

Append to `config_test.go`:

```go
func TestLoad_CacheSudoPasswordDefaultsFalse(t *testing.T) {
	dir := t.TempDir()
	gPath := writeJSON(t, dir, "global.json", Settings{})
	pPath := writeJSON(t, dir, "project.json", Settings{})
	merged, err := Load(gPath, pPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if merged.CacheSudoPassword {
		t.Error("CacheSudoPassword: got true, want false")
	}
}

func TestLoad_CacheSudoPasswordMerge(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name            string
		global, project bool
		want            bool
	}{
		{"global on", true, false, true},
		{"project on", false, true, true},
		{"both off", false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gPath := writeJSON(t, dir, "g-"+tc.name+".json", Settings{CacheSudoPassword: tc.global})
			pPath := writeJSON(t, dir, "p-"+tc.name+".json", Settings{CacheSudoPassword: tc.project})
			merged, err := Load(gPath, pPath)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if merged.CacheSudoPassword != tc.want {
				t.Errorf("got %v, want %v", merged.CacheSudoPassword, tc.want)
			}
		})
	}
}
```

In `skeleton_test.go`, add `"cacheSudoPassword"` to the `required` slice in `TestSkeletonJSON_ContainsAllTopLevelFields`.

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/config/`
Expected: FAIL, compile error `unknown field CacheSudoPassword`.

**Step 3: Implement**

In `Settings` (after `AutoCompactPrompt`):

```go
	// CacheSudoPassword keeps the sudo password in memory for the rest of the
	// process after the first successful entry. Off by default.
	CacheSudoPassword bool `json:"cacheSudoPassword,omitempty"`
```

In the merge function, add to the `merged := Settings{...}` literal:

```go
		CacheSudoPassword:    global.CacheSudoPassword,
```

and after the `if project.AutoCompact {...}` block:

```go
	if project.CacheSudoPassword {
		merged.CacheSudoPassword = true
	}
```

In the sources function, after the `autoCompactPrompt` block:

```go
	if global.CacheSudoPassword {
		sources["cacheSudoPassword"] = SourceGlobal
	}
	if project.CacheSudoPassword {
		sources["cacheSudoPassword"] = SourceProject
	}
```

In `skeleton.go`, change the last two lines of the JSON to:

```
  "autoCompactPrompt": "",
  "cacheSudoPassword": false
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/config/`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/config/
git commit -m "feat(config): add cacheSudoPassword setting"
```

---

### Task 2: Two-step sudo approval key handling

**Files:**
- Modify: `gohome/internal/tui/approval.go` (struct, `newApprovalPrompt`)
- Modify: `gohome/internal/tui/model_approval.go`
- Create: `gohome/internal/tui/sudo_prompt_test.go`
- Modify: `gohome/internal/tui/approval_test.go` (delete three obsolete tests)

**Step 1: Write the failing tests**

Create `gohome/internal/tui/sudo_prompt_test.go`:

```go
package tui

import (
	"encoding/json"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
)

func newSudoTestModel() *Model {
	m := New(nil, "")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

func sendSudoReq(m *Model, sessionID, command string) chan guard.ApprovalDecision {
	ch := make(chan guard.ApprovalDecision, 1)
	input, _ := json.Marshal(map[string]string{"command": command})
	m.Update(ApprovalReqMsg{
		Req: guard.ApprovalRequest{
			SessionID:         sessionID,
			Tool:              "shell",
			Input:             input,
			SuggestedPattern:  "^sudo",
			NeedsSudoPassword: true,
		},
		Reply: ch,
	})
	return ch
}

func pressKey(m *Model, k tea.KeyType) { m.Update(tea.KeyMsg{Type: k}) }

func typeRunes(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func requireNoReply(t *testing.T, ch chan guard.ApprovalDecision) {
	t.Helper()
	select {
	case dec := <-ch:
		t.Fatalf("unexpected reply: %+v", dec)
	default:
	}
}

func requireReply(t *testing.T, ch chan guard.ApprovalDecision) guard.ApprovalDecision {
	t.Helper()
	select {
	case dec := <-ch:
		return dec
	default:
		t.Fatal("expected a reply, got none")
		return guard.ApprovalDecision{}
	}
}

func TestSudo_AllowOpensPasswordStage(t *testing.T) {
	m := newSudoTestModel()
	ch := sendSudoReq(m, "main", "sudo apt install vim")

	typeRunes(m, "1")

	requireNoReply(t, ch)
	if m.activeApproval == nil || !m.activeApproval.sudoStage {
		t.Fatal("expected password stage after Allow once")
	}
}

func TestSudo_PasswordStageAcceptsAllKeys(t *testing.T) {
	m := newSudoTestModel()
	ch := sendSudoReq(m, "main", "sudo apt install vim")
	typeRunes(m, "1")

	typeRunes(m, "1v4e")
	pressKey(m, tea.KeyUp)
	pressKey(m, tea.KeyDown)
	typeRunes(m, "23")
	requireNoReply(t, ch)

	pressKey(m, tea.KeyEnter)
	dec := requireReply(t, ch)
	if dec.Outcome != guard.AllowOnce {
		t.Errorf("outcome: got %q, want AllowOnce", dec.Outcome)
	}
	if dec.SudoPassword != "1v4e23" {
		t.Errorf("password: got %q, want %q", dec.SudoPassword, "1v4e23")
	}
}

func TestSudo_PasswordStagePaste(t *testing.T) {
	m := newSudoTestModel()
	ch := sendSudoReq(m, "main", "sudo true")
	typeRunes(m, "1")

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("pa55word"), Paste: true})
	pressKey(m, tea.KeyEnter)

	if dec := requireReply(t, ch); dec.SudoPassword != "pa55word" {
		t.Errorf("password: got %q, want pa55word", dec.SudoPassword)
	}
}

func TestSudo_EscReturnsToMenu(t *testing.T) {
	m := newSudoTestModel()
	ch := sendSudoReq(m, "main", "sudo true")
	typeRunes(m, "1")
	typeRunes(m, "abc")

	pressKey(m, tea.KeyEsc)

	requireNoReply(t, ch)
	if m.activeApproval == nil || m.activeApproval.sudoStage {
		t.Fatal("expected to be back on the approval menu")
	}
	if v := m.activeApproval.passwordInput.Value(); v != "" {
		t.Errorf("password should be cleared on Esc, got %q", v)
	}

	// From the menu, 3 denies.
	typeRunes(m, "3")
	if dec := requireReply(t, ch); dec.Outcome != guard.Deny {
		t.Errorf("outcome: got %q, want Deny", dec.Outcome)
	}
}

func TestSudo_CtrlCDeniesInPasswordStage(t *testing.T) {
	m := newSudoTestModel()
	ch := sendSudoReq(m, "main", "sudo true")
	typeRunes(m, "1")

	pressKey(m, tea.KeyCtrlC)

	if dec := requireReply(t, ch); dec.Outcome != guard.Deny {
		t.Errorf("outcome: got %q, want Deny", dec.Outcome)
	}
}

func TestSudo_EmptyPasswordShowsError(t *testing.T) {
	m := newSudoTestModel()
	ch := sendSudoReq(m, "main", "sudo true")
	typeRunes(m, "1")

	pressKey(m, tea.KeyEnter)

	requireNoReply(t, ch)
	if m.activeApproval.sudoErr != "Password required" {
		t.Errorf("sudoErr: got %q", m.activeApproval.sudoErr)
	}
	typeRunes(m, "x")
	if m.activeApproval.sudoErr != "" {
		t.Errorf("sudoErr should clear on typing, got %q", m.activeApproval.sudoErr)
	}
}

func TestSudo_AllowAlwaysKeepsPattern(t *testing.T) {
	m := newSudoTestModel()
	ch := sendSudoReq(m, "main", "sudo true")
	typeRunes(m, "2")
	typeRunes(m, "pw")
	pressKey(m, tea.KeyEnter)

	dec := requireReply(t, ch)
	if dec.Outcome != guard.AllowAlways {
		t.Errorf("outcome: got %q, want AllowAlways", dec.Outcome)
	}
	if dec.SavedPattern != "^sudo" {
		t.Errorf("pattern: got %q, want ^sudo", dec.SavedPattern)
	}
	if dec.SudoPassword != "pw" {
		t.Errorf("password: got %q, want pw", dec.SudoPassword)
	}
}

func TestSudo_ArrowEnterOpensPasswordStage(t *testing.T) {
	m := newSudoTestModel()
	ch := sendSudoReq(m, "main", "sudo true")

	pressKey(m, tea.KeyEnter) // default selection is Allow once

	requireNoReply(t, ch)
	if !m.activeApproval.sudoStage {
		t.Fatal("expected password stage after Enter on Allow once")
	}
}

func TestSudo_PgUpInPasswordStageDoesNotTouchField(t *testing.T) {
	m := newSudoTestModel()
	sendSudoReq(m, "main", "sudo true")
	typeRunes(m, "1")
	typeRunes(m, "ab")

	pressKey(m, tea.KeyPgUp)
	pressKey(m, tea.KeyPgDown)

	if v := m.activeApproval.passwordInput.Value(); v != "ab" {
		t.Errorf("password: got %q, want ab", v)
	}
}
```

In `approval_test.go`, delete these three tests, which test the old single-step behaviour and are replaced by the tests above:
- `TestApprovalSudoPasswordFieldShown`
- `TestApprovalSudoPasswordIncludedInDecision`
- `TestApprovalSudoPasswordCachedAcrossPrompts`

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/tui/ -run TestSudo_`
Expected: FAIL, compile errors `m.activeApproval.sudoStage undefined` and `sudoErr undefined`.

**Step 3: Implement**

In `approval.go`, replace the sudo fields of `approvalPrompt`:

```go
	// sudo password sub-mode: command needs sudo password.
	// sudoStage is set after the user picks Allow; while set, every key goes
	// to passwordInput. sudoOutcome remembers which Allow option was picked.
	needsSudo     bool
	sudoStage     bool
	sudoOutcome   guard.ApprovalOutcome
	sudoErr       string
	passwordInput textinput.Model
```

In `newApprovalPrompt`, replace the password input setup with (no focus at creation, no `> ` prompt):

```go
	pwi := textinput.New()
	pwi.Prompt = ""
	pwi.EchoMode = textinput.EchoPassword
```

In `model_approval.go`:

1. `handleApprovalReq`: remove the `sudoPasswordCache` pre-fill (the cache is handled in `allowApproval`, Task 4).

```go
func (m *Model) handleApprovalReq(msg approvalReqMsg) {
	ap := newApprovalPrompt(msg.Req, msg.Reply)
	if m.activeApproval == nil {
		m.activeApproval = ap
	} else {
		m.approvalQueue = append(m.approvalQueue, ap)
	}
}
```

2. Extract the PgUp/PgDn block into a helper and call it from the menu path:

```go
// scrollApprovalPage scrolls the timeline on PgUp/PgDown while an approval is
// active. It reports whether the key was handled.
func (m *Model) scrollApprovalPage(msg tea.KeyMsg) bool {
	if msg.Type != tea.KeyPgUp && msg.Type != tea.KeyPgDown {
		return false
	}
	scrollAmt := m.chat.maxHeight / 2
	if scrollAmt < 1 {
		scrollAmt = 1
	}
	m.chat.DisableAutoScroll(m.winW)
	if msg.Type == tea.KeyPgUp {
		m.chat.ScrollUp(scrollAmt)
	} else {
		m.chat.ScrollDown(scrollAmt)
		m.chat.ReEnableAutoScrollIfAtBottom(m.winW)
	}
	return true
}
```

Replace the inline PgUp/PgDn block in `handleApprovalKey` with:

```go
	// PgUp/PgDown scroll the timeline even during approval.
	if m.scrollApprovalPage(msg) {
		return tea.Batch(cmds...)
	}
```

3. At the very top of `handleApprovalKey`, after `ap := m.activeApproval`, add:

```go
	if ap.sudoStage {
		return m.handleSudoPasswordKey(msg)
	}
```

4. In the top-level menu `switch`, route every Allow path through `allowApproval`:
   - `case 0:` → `cmds = append(cmds, m.allowApproval(guard.AllowOnce))`
   - `case 1:` → `cmds = append(cmds, m.allowApproval(guard.AllowAlways))`
   - `keyRune(msg) == '1'` → `m.allowApproval(guard.AllowOnce)`
   - `keyRune(msg) == '2'` → `m.allowApproval(guard.AllowAlways)`

   Delete the `default:` branch that forwarded keys to `passwordInput`. Leave `case keyRune(msg) == 'e' && !ap.needsSudo:` unchanged.

5. Add the new functions:

```go
// allowApproval resolves an Allow decision. Sudo commands first open the
// password stage; the decision is sent when the password is submitted.
func (m *Model) allowApproval(outcome guard.ApprovalOutcome) tea.Cmd {
	ap := m.activeApproval
	if !ap.needsSudo {
		return m.resolveApproval(m.buildApprovalDecision(outcome))
	}
	ap.sudoStage = true
	ap.sudoOutcome = outcome
	ap.sudoErr = ""
	ap.passwordInput.SetValue("")
	return ap.passwordInput.Focus()
}

// handleSudoPasswordKey routes keys while the password stage is open. Every
// key except Enter, Esc and PgUp/PgDown goes to the password field.
func (m *Model) handleSudoPasswordKey(msg tea.KeyMsg) tea.Cmd {
	ap := m.activeApproval
	if m.scrollApprovalPage(msg) {
		return nil
	}
	switch msg.Type {
	case tea.KeyEnter:
		if ap.passwordInput.Value() == "" {
			ap.sudoErr = "Password required"
			return nil
		}
		return m.resolveApproval(m.buildApprovalDecision(ap.sudoOutcome))
	case tea.KeyEsc:
		ap.sudoStage = false
		ap.sudoErr = ""
		ap.passwordInput.SetValue("")
		ap.passwordInput.Blur()
		return nil
	}
	ap.sudoErr = ""
	var cmd tea.Cmd
	ap.passwordInput, cmd = ap.passwordInput.Update(msg)
	return cmd
}
```

6. In `resolveApproval`, only write the cache when the setting is on:

```go
	if m.settings.CacheSudoPassword && m.activeApproval.needsSudo && dec.SudoPassword != "" {
		m.sudoPasswordCache = dec.SudoPassword
	}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/tui/ -run 'TestSudo_|TestApproval'`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/tui/approval.go gohome/internal/tui/model_approval.go gohome/internal/tui/sudo_prompt_test.go gohome/internal/tui/approval_test.go
git commit -m "fix(tui): two-step sudo approval so password keys never hit menu shortcuts"
```

---

### Task 3: Prominent password dialog and status bar hint

**Files:**
- Modify: `gohome/internal/tui/approval.go` (`renderApprovalOverlay`, new `renderSudoDialog`)
- Modify: `gohome/internal/tui/statusbar.go:101-110`
- Modify: `gohome/internal/tui/tui_snapshot_test.go` (~line 117)
- Test: `gohome/internal/tui/sudo_prompt_test.go`
- Golden: `gohome/internal/tui/testdata/TestSnapshots/sudo_approval_prompt.golden` (regenerated), plus two new goldens

**Step 1: Write the failing tests**

Append to `sudo_prompt_test.go` (add `"strings"` to imports):

```go
func TestSudo_DialogRender(t *testing.T) {
	m := newSudoTestModel()
	sendSudoReq(m, "main", "sudo apt install vim")

	menu := StripAnsi(m.View())
	if strings.Contains(menu, "Password:") {
		t.Error("menu stage should not show the password field")
	}

	typeRunes(m, "1")
	typeRunes(m, "abc")
	view := StripAnsi(m.View())
	for _, want := range []string{
		"SUDO PASSWORD REQUIRED",
		"sudo apt install vim",
		"Password: ***",
		"Enter: run | Esc: back | Ctrl+C: deny",
		"sudo password needed",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "abc") {
		t.Error("password text must not be rendered")
	}
}

func TestSudo_DialogShowsSubagentLabel(t *testing.T) {
	m := newSudoTestModel()
	sendSudoReq(m, "sub-1", "sudo true")
	typeRunes(m, "1")

	if view := StripAnsi(m.View()); !strings.Contains(view, "[sub-1] SUDO PASSWORD REQUIRED") {
		t.Errorf("missing subagent label:\n%s", view)
	}
}
```

In `tui_snapshot_test.go`, after the existing `sudo_approval_prompt` subtest, add:

```go
	// (d3) Sudo password dialog after choosing Allow once.
	t.Run("sudo_password_dialog", func(t *testing.T) {
		m := newSized()
		reply := make(chan guard.ApprovalDecision, 1)
		m = apply(m, tui.ApprovalReqMsg{
			Req: guard.ApprovalRequest{
				SessionID:         "main",
				Tool:              "shell",
				Input:             []byte(`{"command":"sudo apt install vim"}`),
				SuggestedPattern:  "^sudo",
				NeedsSudoPassword: true,
			},
			Reply: reply,
		})
		m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
		m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("pw")})
		golden.RequireEqual(t, []byte(m.View()))
	})

	// (d4) Sudo password dialog from a subagent.
	t.Run("sudo_password_dialog_subagent", func(t *testing.T) {
		m := newSized()
		reply := make(chan guard.ApprovalDecision, 1)
		m = apply(m, tui.ApprovalReqMsg{
			Req: guard.ApprovalRequest{
				SessionID:         "sub-1",
				Tool:              "shell",
				Input:             []byte(`{"command":"sudo systemctl restart nginx"}`),
				SuggestedPattern:  "^sudo",
				NeedsSudoPassword: true,
			},
			Reply: reply,
		})
		m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
		golden.RequireEqual(t, []byte(m.View()))
	})
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/tui/ -run 'TestSudo_Dialog'`
Expected: FAIL, view missing `SUDO PASSWORD REQUIRED`.

**Step 3: Implement**

In `approval.go`:

1. Remove the `if ap.needsSudo { ... Password: ... }` block from `renderApprovalOverlay`.
2. At the top of `renderApprovalOverlay`, add:

```go
	if ap.sudoStage {
		return renderSudoDialog(ap, width, focusedSessionID)
	}
```

3. Add:

```go
var (
	sudoBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			Padding(0, 1).
			BorderForeground(lipgloss.Color("9"))
	sudoHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
)

// renderSudoDialog renders the password stage of a sudo approval.
func renderSudoDialog(ap *approvalPrompt, width int, focusedSessionID string) string {
	boxW := width - 4
	if boxW < 20 {
		boxW = 20
	}

	header := "SUDO PASSWORD REQUIRED"
	if ap.req.SessionID != focusedSessionID {
		header = fmt.Sprintf("[%s] %s", ap.req.SessionID, header)
	}

	const maxCmdLines = 3
	cmdLines := WrapText(extractToolArg(ap.req.Tool, string(ap.req.Input)), boxW)
	if len(cmdLines) > maxCmdLines {
		cmdLines = cmdLines[:maxCmdLines]
		cmdLines[maxCmdLines-1] += " ..."
	}

	var sb strings.Builder
	sb.WriteString(sudoHeaderStyle.Render(header))
	sb.WriteString("\n")
	sb.WriteString(strings.Join(cmdLines, "\n"))
	sb.WriteString("\n\n")
	sb.WriteString("Password: ")
	sb.WriteString(ap.passwordInput.View())
	sb.WriteString("\n")
	sb.WriteString(ap.sudoErr)
	sb.WriteString("\n")
	sb.WriteString("Enter: run | Esc: back | Ctrl+C: deny")

	return sudoBoxStyle.Width(boxW).Render(sb.String())
}
```

In `statusbar.go`, make the sudo hint take priority over the other right-side hints:

```go
	var right string
	if m.activeApproval != nil && m.activeApproval.sudoStage {
		right = "sudo password needed"
	} else if !m.mouseHintUntil.IsZero() && time.Now().Before(m.mouseHintUntil) {
```

(the rest of the chain stays the same).

**Step 4: Regenerate goldens and run tests**

Run: `go test ./gohome/internal/tui/ -run TestSnapshots -update`
Then: `git diff gohome/internal/tui/testdata/`
Expected: `sudo_approval_prompt.golden` loses the `Password: >` line. New `sudo_password_dialog*.golden` files show a double-line box with the header, command, `Password: **` and hint, and the status bar ends with `sudo password needed`. No other golden changes.

Run: `go test ./gohome/internal/tui/`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/tui/
git commit -m "feat(tui): prominent sudo password dialog"
```

---

### Task 4: Opt-in cache, cached-password notice, wrong-password clearing

**Files:**
- Modify: `gohome/internal/tui/model_approval.go` (`allowApproval`, new `sudoRejected`)
- Modify: `gohome/internal/tui/model_agent.go` (`EventToolResult` case, ~line 115)
- Test: `gohome/internal/tui/sudo_prompt_test.go`

**Step 1: Write the failing tests**

Append to `sudo_prompt_test.go` (add imports `"github.com/jhyoong/GoHome/gohome/internal/agent"` and `"github.com/jhyoong/GoHome/gohome/internal/config"`):

```go
func enterSudoPassword(t *testing.T, m *Model, command, password string) guard.ApprovalDecision {
	t.Helper()
	ch := sendSudoReq(m, "main", command)
	typeRunes(m, "1")
	typeRunes(m, password)
	pressKey(m, tea.KeyEnter)
	return requireReply(t, ch)
}

func lastNotice(m *Model) string {
	tl := m.sessions[m.focused].Timeline
	for i := len(tl) - 1; i >= 0; i-- {
		if tl[i].Kind == KindNotice {
			return tl[i].Text
		}
	}
	return ""
}

func TestSudo_CacheOffAlwaysAsks(t *testing.T) {
	m := newSudoTestModel()
	enterSudoPassword(t, m, "sudo true", "pw")

	ch := sendSudoReq(m, "main", "sudo ls")
	typeRunes(m, "1")
	requireNoReply(t, ch)
	if !m.activeApproval.sudoStage {
		t.Fatal("cache off: expected password stage on second sudo")
	}
	if m.sudoPasswordCache != "" {
		t.Error("cache off: password must not be stored")
	}
}

func TestSudo_CacheOnSkipsDialog(t *testing.T) {
	m := newSudoTestModel()
	m.SetSettings(config.Settings{CacheSudoPassword: true})
	enterSudoPassword(t, m, "sudo true", "pw")

	ch := sendSudoReq(m, "main", "sudo ls")
	typeRunes(m, "1")

	dec := requireReply(t, ch)
	if dec.Outcome != guard.AllowOnce || dec.SudoPassword != "pw" {
		t.Errorf("got %+v, want AllowOnce with cached pw", dec)
	}
	if got := lastNotice(m); got != "Using cached sudo password" {
		t.Errorf("notice: got %q", got)
	}
}

func TestSudo_RejectedPasswordClearsCache(t *testing.T) {
	m := newSudoTestModel()
	m.SetSettings(config.Settings{CacheSudoPassword: true})
	enterSudoPassword(t, m, "sudo true", "wrong")

	m.Update(AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventToolResult,
		SessionID: "main",
		Result: &agent.ToolResult{
			Content: "Password:Sorry, try again.\nsudo: no password was provided\n",
			IsError: true,
		},
	}})

	if m.sudoPasswordCache != "" {
		t.Error("cache should be cleared after rejection")
	}
	if got := lastNotice(m); got != "Cached sudo password was rejected and cleared" {
		t.Errorf("notice: got %q", got)
	}

	ch := sendSudoReq(m, "main", "sudo ls")
	typeRunes(m, "1")
	requireNoReply(t, ch)
	if !m.activeApproval.sudoStage {
		t.Fatal("expected password stage after cache was cleared")
	}
}

func TestSudoRejected(t *testing.T) {
	cases := map[string]bool{
		"Sorry, try again.":                          true,
		"sudo: 3 incorrect password attempts":        true,
		"sudo: 1 incorrect password attempt":         true,
		"Reading package lists... Done":              false,
		"":                                           false,
	}
	for in, want := range cases {
		if got := sudoRejected(in); got != want {
			t.Errorf("sudoRejected(%q) = %v, want %v", in, got, want)
		}
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/tui/ -run 'TestSudo_Cache|TestSudo_Rejected|TestSudoRejected'`
Expected: FAIL, `undefined: sudoRejected`.

**Step 3: Implement**

In `allowApproval` (`model_approval.go`), after the `!ap.needsSudo` early return, add:

```go
	if m.settings.CacheSudoPassword && m.sudoPasswordCache != "" {
		m.AddTimelineEntry(ap.req.SessionID, TimelineEntry{Kind: KindNotice, Text: "Using cached sudo password"})
		dec := m.buildApprovalDecision(outcome)
		dec.SudoPassword = m.sudoPasswordCache
		return m.resolveApproval(dec)
	}
```

Add to `model_approval.go`:

```go
// sudoRejected reports whether shell output contains sudo's standard
// wrong-password messages.
func sudoRejected(output string) bool {
	return strings.Contains(output, "Sorry, try again") ||
		strings.Contains(output, "incorrect password attempt")
}
```

In `model_agent.go`, at the end of the `case agent.EventToolResult:` block (after the shadow update):

```go
		if m.sudoPasswordCache != "" && sudoRejected(content) {
			m.sudoPasswordCache = ""
			sv.Timeline = append(sv.Timeline, TimelineEntry{
				Kind: KindNotice,
				Text: "Cached sudo password was rejected and cleared",
			})
		}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/tui/`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/tui/
git commit -m "feat(tui): opt-in sudo password cache with rejection handling"
```

---

### Task 5: Keep mouse capture on while an approval is shown

Reason: when capture is off, many terminals send wheel scrolls as Up/Down keys, which move the approval menu selection. Trade-off: native text selection (without Shift) is not available while an approval is open.

**Files:**
- Modify: `gohome/internal/tui/model.go` (`approvalReqMsg` case ~line 444, `mouseIdleMsg` case ~line 450, wheel case ~line 472)
- Modify: `gohome/internal/tui/model_approval.go` (`handleApprovalReq`, `resolveApproval`)
- Test: `gohome/internal/tui/sudo_prompt_test.go`

**Step 1: Write the failing tests**

```go
func TestApprovalReenablesMouseCapture(t *testing.T) {
	m := newSudoTestModel()
	m.mouseActive = false

	ch := make(chan guard.ApprovalDecision, 1)
	_, cmd := m.Update(ApprovalReqMsg{
		Req:   guard.ApprovalRequest{SessionID: "main", Tool: "shell", Input: json.RawMessage(`{"command":"ls"}`)},
		Reply: ch,
	})

	if !m.mouseActive {
		t.Error("expected mouse capture re-enabled when approval arrives")
	}
	if cmd == nil {
		t.Error("expected a command to enable mouse capture")
	}
}

func TestMouseIdleIgnoredDuringApproval(t *testing.T) {
	m := newSudoTestModel()
	sendSudoReq(m, "main", "sudo true")
	m.mouseIdleSeq = 5

	m.Update(mouseIdleMsg{seq: 5})

	if !m.mouseActive {
		t.Error("mouse capture must stay on while an approval is active")
	}
}

func TestResolveApprovalSchedulesMouseIdle(t *testing.T) {
	m := newSudoTestModel()
	ch := sendSudoReq(m, "main", "sudo true")
	before := m.mouseIdleSeq

	typeRunes(m, "3")
	requireReply(t, ch)

	if m.mouseIdleSeq == before {
		t.Error("expected a fresh mouse idle timer after resolving the approval")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/tui/ -run 'Mouse'`
Expected: FAIL on all three.

**Step 3: Implement**

In `model.go`, add a helper and use it in the wheel handler (replacing the inline `m.mouseIdleSeq++ ... tea.Tick(...)` lines):

```go
// scheduleMouseIdle starts a fresh idle timer; older timers become stale.
func (m *Model) scheduleMouseIdle() tea.Cmd {
	m.mouseIdleSeq++
	seq := m.mouseIdleSeq
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return mouseIdleMsg{seq: seq}
	})
}
```

Wheel handler becomes: `cmds = append(cmds, m.scheduleMouseIdle())`.

Change the `mouseIdleMsg` case condition to:

```go
		if msg.seq == m.mouseIdleSeq && m.mouseEnabled && m.mouseActive && m.activeApproval == nil {
```

Change `handleApprovalReq` to return a command:

```go
func (m *Model) handleApprovalReq(msg approvalReqMsg) tea.Cmd {
	ap := newApprovalPrompt(msg.Req, msg.Reply)
	if m.activeApproval == nil {
		m.activeApproval = ap
	} else {
		m.approvalQueue = append(m.approvalQueue, ap)
	}
	// Keep wheel events as mouse events so they cannot move the menu.
	if m.mouseEnabled && !m.mouseActive {
		m.mouseActive = true
		return func() tea.Msg { return tea.EnableMouseCellMotion() }
	}
	return nil
}
```

and the `Update` case:

```go
	case approvalReqMsg:
		if cmd := m.handleApprovalReq(msg); cmd != nil {
			return m, cmd
		}
```

In `resolveApproval`, collect commands and schedule the idle timer when the last approval closes:

```go
	m.activeApproval.reply <- dec
	m.activeApproval = nil
	m.promoteApproval()

	var cmds []tea.Cmd
	if m.activeApproval == nil && m.mouseEnabled && m.mouseActive {
		cmds = append(cmds, m.scheduleMouseIdle())
	}
	if m.activeApproval == nil && (dec.Outcome == guard.AllowOnce || dec.Outcome == guard.AllowAlways) {
		m.spinner.Start("Processing...")
		m.spinner.SetOnCancel(m.cancelFocusedSession)
		cmds = append(cmds, m.spinnerTickCmd())
	}
	return tea.Batch(cmds...)
```

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/tui/`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/tui/
git commit -m "fix(tui): keep mouse capture on during approvals so wheel cannot move the menu"
```

---

### Task 6: Cap chat height so the view never exceeds the terminal

**Files:**
- Modify: `gohome/internal/tui/model.go` (`render`, ~lines 653-715)
- Test: `gohome/internal/tui/sudo_prompt_test.go`

**Step 1: Write the failing test**

```go
func TestViewFitsWindowWithApprovals(t *testing.T) {
	m := newSudoTestModel()
	// Fill the timeline so the chat wants all available height.
	for i := 0; i < 50; i++ {
		m.AddTimelineEntry("main", TimelineEntry{Kind: KindNotice, Text: "line"})
	}

	check := func(stage string) {
		t.Helper()
		lines := strings.Count(m.View(), "\n") + 1
		if lines > 24 {
			t.Errorf("%s: view has %d lines, window is 24", stage, lines)
		}
	}

	sendSudoReq(m, "main", "sudo apt install vim")
	check("approval menu")
	typeRunes(m, "v")
	check("expanded menu")
	typeRunes(m, "1")
	check("password dialog")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./gohome/internal/tui/ -run TestViewFitsWindowWithApprovals`
Expected: FAIL, "view has 2x lines, window is 24".

**Step 3: Implement**

Restructure the non-overlay part of `render()`. Build every section below the chat first, then size the chat from what is left. Keep the existing order of sections in the final output.

```go
	// Sections below the chat, built first so the chat can be sized to fit.
	var below []string
	if spinnerLines := m.spinner.Render(m.winW); len(spinnerLines) > 0 {
		below = append(below, strings.Join(spinnerLines, "\n"))
	}
	if m.fileSearching {
		if popupLines := m.fileSearch.Render(m.winW); len(popupLines) > 0 {
			below = append(below, strings.Join(popupLines, "\n"))
		}
	}
	if pendingLines := m.pending.Render(m.winW); len(pendingLines) > 0 {
		below = append(below, strings.Join(pendingLines, "\n"))
	}
	if m.statusMsg != "" {
		below = append(below, m.statusMsg)
	}
	below = append(below, m.renderInputRegion())
	below = append(below, m.statusBar())

	// Chat area: the usual height, capped so the frame fits the window.
	chatH := m.winH - editorMinHeight - 2 - stripHeight - statusHeight - 2
	if avail := m.winH - countLines(sections) - countLines(below); avail < chatH {
		chatH = avail
	}
	if chatH < 1 {
		chatH = 1
	}
	m.chat.SetMaxHeight(chatH)
	if sv, ok := m.sessions[m.focused]; ok {
		m.chat.SetTimeline(&sv.Timeline)
	}
	if chatLines := m.chat.Render(m.winW); len(chatLines) > 0 {
		sections = append(sections, strings.Join(chatLines, "\n"))
	}

	sections = append(sections, below...)
	return strings.Join(sections, "\n")
}

// renderInputRegion renders the swappable input slot: approval prompt,
// modal, completed label, or the editor.
func (m *Model) renderInputRegion() string {
	if m.activeApproval != nil {
		return renderApprovalOverlay(m.activeApproval, m.winW, m.focused)
	}
	if m.activeModal != nil {
		return strings.Join(m.activeModal.Render(m.winW), "\n")
	}
	if sv, ok := m.sessions[m.focused]; ok && sv.Completed {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("[Session complete]")
	}
	var parts []string
	if palette := m.slashPalette(); palette != "" {
		parts = append(parts, palette)
	}
	m.editor.SetTermHeight(m.winH)
	parts = append(parts, strings.Join(m.editor.Render(m.winW), "\n"))
	return strings.Join(parts, "\n")
}

// countLines returns the number of terminal lines the joined sections use.
func countLines(sections []string) int {
	n := 0
	for _, s := range sections {
		n += strings.Count(s, "\n") + 1
	}
	return n
}
```

Notes:
- `sections` at this point holds only the session strip and the optional notification line.
- The cap only shrinks the chat. Goldens where everything already fits must not change.

**Step 4: Run tests**

Run: `go test ./gohome/internal/tui/`
Expected: PASS. If `TestSnapshots` fails, run with `-update` and check `git diff gohome/internal/tui/testdata/`. Only the sudo/approval goldens should change (fewer chat lines, session strip still on line 1). Any other golden change means a regression to fix before continuing.

**Step 5: Commit**

```bash
git add gohome/internal/tui/
git commit -m "fix(tui): size chat to the real input region so approvals never overflow"
```

---

### Task 7: Wizard step for sudo caching

**Files:**
- Modify: `gohome/internal/tui/config_wizard.go`
- Test: `gohome/internal/tui/config_wizard_test.go`

**Step 1: Write the failing tests**

In `TestConfigWizard_FullFlow`, change the "Step 6" assertion and insert a new step before confirm:

```go
	w.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})
	if w.step != wizardStepSudoCache {
		t.Fatalf("after config name: step = %d, want %d", w.step, wizardStepSudoCache)
	}

	// Step 7: sudo cache -- default "No" (first item)
	w.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})
	if w.step != wizardStepConfirm {
		t.Fatalf("after sudo cache: step = %d, want %d", w.step, wizardStepConfirm)
	}

	// Step 8: confirm (select "Save", first item)
```

After the existing field assertions in that test, add:

```go
	if s.CacheSudoPassword {
		t.Error("cacheSudoPassword: got true, want false (default No)")
	}
```

Add a new test:

```go
func TestConfigWizard_SudoCacheYes(t *testing.T) {
	dir := t.TempDir()
	outPath := dir + "/settings.json"
	w := NewConfigWizard(func() {}, func(string) {})
	w.outputPath = outPath
	w.wire, w.baseURL, w.keySource, w.keyValue = "anthropic", "http://x", "env", "K"
	w.modelName, w.configName = "m", "c"
	w.step = wizardStepSudoCache
	w.buildStep()

	if joined := StripAnsi(strings.Join(w.Render(80), "\n")); !strings.Contains(joined, "Cache sudo password") {
		t.Fatalf("missing prompt:\n%s", joined)
	}

	w.HandleInput(tea.KeyMsg{Type: tea.KeyDown})  // move to "Yes"
	w.HandleInput(tea.KeyMsg{Type: tea.KeyEnter}) // select
	if !strings.Contains(w.summaryText(), "Sudo cache:   on") {
		t.Errorf("summary missing sudo cache line:\n%s", w.summaryText())
	}
	w.HandleInput(tea.KeyMsg{Type: tea.KeyEnter}) // Save

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var s config.Settings
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !s.CacheSudoPassword {
		t.Error("cacheSudoPassword: got false, want true")
	}
}

func TestConfigWizard_ConfirmEscGoesBackToSudoCache(t *testing.T) {
	w := NewConfigWizard(func() {}, func(string) {})
	w.step = wizardStepConfirm
	w.buildStep()
	w.HandleInput(tea.KeyMsg{Type: tea.KeyEsc})
	if w.step != wizardStepSudoCache {
		t.Errorf("step = %d, want %d", w.step, wizardStepSudoCache)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./gohome/internal/tui/ -run TestConfigWizard`
Expected: FAIL, `undefined: wizardStepSudoCache`.

**Step 3: Implement**

In `config_wizard.go`:

1. Insert `wizardStepSudoCache` between `wizardStepConfigName` and `wizardStepConfirm` in the const block.
2. Add field `sudoCache bool` to `ConfigWizard`.
3. In `buildStep`, add:

```go
	case wizardStepSudoCache:
		w.prompt = "Cache sudo password in memory for the session?"
		items := []SelectItem{
			{Value: "no", Label: "No", Description: "ask for the password on every sudo command"},
			{Value: "yes", Label: "Yes", Description: "ask once, reuse until gohome exits"},
		}
		w.selectList = NewSelectList(items, func(item SelectItem) {
			w.sudoCache = item.Value == "yes"
			w.step++
			w.buildStep()
		})
		w.selectList.onCancel = func() {
			w.step--
			w.buildStep()
		}
```

4. In `summaryText`, after the model name line:

```go
	sudo := "off"
	if w.sudoCache {
		sudo = "on"
	}
	fmt.Fprintf(&sb, "  Sudo cache:   %s\n", sudo)
```

5. In `save`, set the field on the settings literal:

```go
	s := config.Settings{
		ModelConfig:       map[string]config.ModelConfig{w.configName: mc},
		DefaultModel:      w.configName,
		CacheSudoPassword: w.sudoCache,
	}
```

6. In `Render`, change `"Setup Wizard (step %d/9)"` to `"Setup Wizard (step %d/10)"`.

**Step 4: Run tests to verify they pass**

Run: `go test ./gohome/internal/tui/`
Expected: PASS

**Step 5: Commit**

```bash
git add gohome/internal/tui/config_wizard.go gohome/internal/tui/config_wizard_test.go
git commit -m "feat(wizard): add sudo password cache step"
```

---

### Task 8: Documentation

**Files:**
- Modify: `README.md` (section "Guardrails and approval", ~line 276)

**Step 1: Add a sudo subsection** directly after the `/yolo` line in "Guardrails and approval":

```markdown
### Sudo commands

When a shell command uses `sudo`, the approval menu works as normal. After you
choose **Allow once** or **Allow always**, a password dialog opens. Every key
goes to the password field. Enter runs the command, Esc goes back to the menu,
Ctrl+C denies.

By default the password is asked for every sudo command. To reuse it until
`gohome` exits, set:

```json
{ "cacheSudoPassword": true }
```

The password is kept in memory only. If sudo rejects a cached password, the
cache is cleared and the next sudo command asks again.
```

**Step 2: Commit**

```bash
git add README.md
git commit -m "docs: describe sudo password dialog and cacheSudoPassword"
```

---

### Task 9: Full verification

**Step 1:** `go vet ./gohome/...` -- expected: no output.

**Step 2:** `golangci-lint run ./gohome/...` -- expected: no issues.

**Step 3:** `go test ./gohome/...` -- expected: all PASS.

**Step 4:** `go build -ldflags "-X main.version=dev" -o bin/gohome ./gohome/cmd/gohome` -- expected: builds.

**Step 5: Manual check (user runs this; needs a real terminal and sudo).**

1. Run `./bin/gohome --model <name>` and ask the agent to run `sudo -k; sudo true`.
2. In the approval menu, scroll the wheel and use PgUp/PgDn. Confirm the `>` marker does not move.
3. Press `1`. Confirm the red double-bordered dialog appears with `sudo password needed` in the status bar.
4. Type a password containing digits and `v`/`e`, scroll and PgUp/PgDn during typing, press Enter. Confirm the command succeeds.
5. Repeat with a wrong password. Confirm the command fails.
6. With `"cacheSudoPassword": true`, confirm the second sudo command skips the dialog and shows `Using cached sudo password`, and that a rejected cached password shows the cleared notice.
7. Resize the terminal small (e.g. 80x20) with the dialog open. Confirm the session strip stays on the top line.

---

## Addendum (2026-10-03): whitelisted sudo commands

Found in final review: after "Allow always" on a sudo command, the pattern is
whitelisted, so later sudo commands skip approval and run with no password.
`sudo` then opens `/dev/tty` and prompts underneath the TUI, fighting it for
keystrokes. User decision: whitelisted sudo commands skip the menu but still
open the password dialog (or use the cache), and shell commands run in their
own session so nothing can take over the terminal.

### Task 10: Run shell commands without a controlling terminal

**Files:**
- Create: `gohome/internal/tools/shell_unix.go` (`//go:build !windows`)
- Create: `gohome/internal/tools/shell_windows.go` (`//go:build windows`)
- Modify: `gohome/internal/tools/shell.go` (after the `exec.CommandContext` block)
- Test: `gohome/internal/tools/shell_unix_test.go` (`//go:build !windows`)

`shell_unix.go`:

```go
// detachFromTerminal starts the command in a new session with no controlling
// terminal, so programs that open /dev/tty (sudo, ssh password prompts) fail
// instead of drawing over the TUI and stealing keystrokes.
func detachFromTerminal(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
```

`shell_windows.go`: same signature, no-op body.

`shell.go`: call `detachFromTerminal(cmd)` right after `cmd` is created.

Test: run the shell tool (same way `shell_test.go` does) with
`[ "$(ps -o sid= -p $$ | tr -d ' ')" = "$$" ] && echo SESSION_LEADER`
and assert the output contains `SESSION_LEADER`. Write it first and confirm it
fails before the change.

Commit: `fix(shell): run commands in their own session so they cannot grab the terminal`

### Task 11: Password-only prompt for whitelisted sudo commands

**Files:**
- Modify: `gohome/internal/guard/guard.go` (`ApprovalRequest`)
- Modify: `gohome/internal/guard/check.go` (whitelist step)
- Modify: `gohome/internal/tui/approval.go`, `gohome/internal/tui/model_approval.go`
- Modify: `README.md` (Sudo commands section)
- Test: `gohome/internal/guard/guard_test.go`, `gohome/internal/tui/sudo_prompt_test.go`

Guard:
- Add `PasswordOnly bool` to `ApprovalRequest`: the command is already
  whitelisted; the frontend only collects the sudo password. `AllowOnce` (or
  `AllowAlways`) with a password runs it; anything else blocks it.
- In `Check`, when the whitelist allows the call and it is a shell sudo
  command, call the frontend with `NeedsSudoPassword: true, PasswordOnly: true`
  and return `Decision{Allow: true, Reason: "whitelisted", SudoPassword: ...}`
  for an allow outcome, or `Decision{Allow: false, Reason: "user_denied"}`
  otherwise. Non-sudo whitelisted calls still skip the frontend. Yolo is
  unchanged (with Task 10, sudo under yolo fails cleanly).
- Tests: whitelisted sudo calls the frontend with both flags and threads the
  password; whitelisted sudo + Deny is blocked; whitelisted non-sudo still makes
  no frontend call.

TUI:
- `newApprovalPrompt`: when `req.PasswordOnly`, start in the password stage
  (`sudoStage = true`, `sudoOutcome = AllowOnce`, field focused).
- `handleApprovalReq`: when `req.PasswordOnly` and the cache is enabled and
  set, reply immediately with `AllowOnce` + cached password, add the
  "Using cached sudo password" notice, and do not enqueue a prompt.
- `handleSudoPasswordKey`: Esc on a password-only prompt denies (there is no
  menu to return to).
- `renderSudoDialog`: hint is `Enter: run | Esc: deny` for password-only
  prompts.
- Tests: password-only request opens the dialog at once; typing + Enter
  replies AllowOnce with the password; Esc denies; with the cache on and set,
  the reply is immediate, no prompt becomes active, and the notice is added;
  hint text differs.

README: in "Sudo commands", say that sudo commands allowed by the whitelist
skip the menu but still ask for the password (or use the cache), and that shell
commands run without a terminal, so programs that prompt on the terminal fail
instead of taking over the screen.

Commit: `fix: ask for the sudo password even when the command is whitelisted`
