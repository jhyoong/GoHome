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
