package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/config"
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
	typeRunes(m, "secret")

	pressKey(m, tea.KeyCtrlC)

	dec := requireReply(t, ch)
	if dec.Outcome != guard.Deny {
		t.Errorf("outcome: got %q, want Deny", dec.Outcome)
	}
	if dec.SudoPassword != "" {
		t.Errorf("password must not be sent on deny, got %q", dec.SudoPassword)
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

func TestSudo_DialogShowsError(t *testing.T) {
	m := newSudoTestModel()
	sendSudoReq(m, "main", "sudo true")
	typeRunes(m, "1")
	pressKey(m, tea.KeyEnter)

	if view := StripAnsi(m.View()); !strings.Contains(view, "Password required") {
		t.Errorf("missing error line:\n%s", view)
	}
}

// dialogHeight returns the number of lines between the sudo dialog's top and
// bottom borders (inclusive).
func dialogHeight(t *testing.T, view string) int {
	t.Helper()
	lines := strings.Split(view, "\n")
	top, bottom := -1, -1
	for i, l := range lines {
		if strings.HasPrefix(l, "╔") {
			top = i
		}
		if strings.HasPrefix(l, "╚") {
			bottom = i
		}
	}
	if top < 0 || bottom < 0 {
		t.Fatalf("dialog borders not found:\n%s", view)
	}
	return bottom - top + 1
}

func requireMaxWidth(t *testing.T, view string, maxW int) {
	t.Helper()
	for i, l := range strings.Split(view, "\n") {
		if w := VisualWidth(l); w > maxW {
			t.Errorf("line %d is %d wide (max %d): %q", i, w, maxW, l)
		}
	}
}

func TestSudo_LongCommandWrapsToContentWidth(t *testing.T) {
	short := "sudo true"
	// Both are longer than 3*74 columns; one with spaces, one without.
	longWords := "sudo echo " + strings.Repeat("abcdefghi ", 40)
	longSolid := "sudo " + strings.Repeat("x", 400)

	m := newSudoTestModel()
	sendSudoReq(m, "main", short)
	typeRunes(m, "1")
	// Short command: header + 1 cmd line + blank + password + err + hint + 2 borders.
	if h := dialogHeight(t, StripAnsi(m.View())); h != 8 {
		t.Fatalf("short command dialog height = %d, want 8", h)
	}

	for _, cmd := range []string{longWords, longSolid} {
		m := newSudoTestModel()
		sendSudoReq(m, "main", cmd)

		menu := StripAnsi(m.View())
		requireMaxWidth(t, menu, 80)
		if !strings.Contains(menu, "(v to expand)") {
			t.Errorf("menu stage should be truncated:\n%s", menu)
		}

		typeRunes(m, "1")
		view := StripAnsi(m.View())
		requireMaxWidth(t, view, 80)
		// Command capped at 3 lines: 2 more than the short command.
		if h := dialogHeight(t, view); h != 10 {
			t.Errorf("long command dialog height = %d, want 10:\n%s", h, view)
		}
		if !strings.Contains(view, " ...") {
			t.Errorf("truncated command should end with ...:\n%s", view)
		}
	}
}

func TestSudo_LongPasswordStaysOnOneLine(t *testing.T) {
	m := newSudoTestModel()
	sendSudoReq(m, "main", "sudo true")
	typeRunes(m, "1")
	before := dialogHeight(t, StripAnsi(m.View()))
	typeRunes(m, strings.Repeat("p", 200))
	view := StripAnsi(m.View())
	requireMaxWidth(t, view, 80)
	if h := dialogHeight(t, view); h != before {
		t.Errorf("dialog height changed from %d to %d with a long password", before, h)
	}
}

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

	sendToolRoundtrip(m, "shell", "call-1", "Password:Sorry, try again.\nsudo: no password was provided\n")

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

// sendToolRoundtrip sends a tool call event followed by its result.
func sendToolRoundtrip(m *Model, tool, callID, result string) {
	m.Update(AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:       agent.EventToolCallDone,
		SessionID:  "main",
		ToolName:   tool,
		ToolCallID: callID,
		InputJSON:  `{}`,
	}})
	m.Update(AgentEventMsg{SessionID: "main", Ev: agent.Event{
		Kind:      agent.EventToolResult,
		SessionID: "main",
		Result:    &agent.ToolResult{ToolUseID: callID, Content: result, IsError: true},
	}})
}

func TestSudo_NonShellResultKeepsCache(t *testing.T) {
	m := newSudoTestModel()
	m.SetSettings(config.Settings{CacheSudoPassword: true})
	enterSudoPassword(t, m, "sudo true", "pw")

	sendToolRoundtrip(m, "read", "call-1", "notes.txt: Sorry, try again.")

	if m.sudoPasswordCache != "pw" {
		t.Errorf("cache should be kept for non-shell results, got %q", m.sudoPasswordCache)
	}
	if got := lastNotice(m); got == "Cached sudo password was rejected and cleared" {
		t.Error("unexpected rejection notice for non-shell result")
	}
}

func TestSudoRejected(t *testing.T) {
	cases := map[string]bool{
		"Sorry, try again.":                   true,
		"sudo: 3 incorrect password attempts": true,
		"sudo: 1 incorrect password attempt":  true,
		"Reading package lists... Done":       false,
		"":                                    false,
	}
	for in, want := range cases {
		if got := sudoRejected(in); got != want {
			t.Errorf("sudoRejected(%q) = %v, want %v", in, got, want)
		}
	}
}

// A queued sudo approval must start fresh after the first is resolved.
func TestSudo_QueuedApprovalStartsFresh(t *testing.T) {
	m := newSudoTestModel()
	ch1 := sendSudoReq(m, "main", "sudo true")
	ch2 := sendSudoReq(m, "main", "sudo ls")

	typeRunes(m, "1")
	typeRunes(m, "pw")
	pressKey(m, tea.KeyEnter)
	requireReply(t, ch1)

	requireNoReply(t, ch2)
	ap := m.activeApproval
	if ap == nil {
		t.Fatal("expected queued approval to become active")
	}
	if ap.sudoStage || ap.passwordInput.Value() != "" || ap.sudoErr != "" {
		t.Errorf("queued approval not fresh: stage=%v value=%q err=%q",
			ap.sudoStage, ap.passwordInput.Value(), ap.sudoErr)
	}
}
