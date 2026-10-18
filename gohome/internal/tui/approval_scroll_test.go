package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
)

func newLongCommandModel(t *testing.T) *Model {
	t.Helper()
	m := New(nil, "")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	var lines []string
	for i := 0; i < 42; i++ {
		lines = append(lines, fmt.Sprintf("LINE_%02d", i))
	}
	input, _ := json.Marshal(map[string]string{"command": strings.Join(lines, "\n")})
	m.Update(ApprovalReqMsg{
		Req:   guard.ApprovalRequest{SessionID: "main", Tool: "shell", Input: input},
		Reply: make(chan guard.ApprovalDecision, 1),
	})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	return m
}

func TestApprovalExpandedLongCommandIsCappedAndScrolls(t *testing.T) {
	m := newLongCommandModel(t)

	view := StripAnsi(m.View())
	if !strings.Contains(view, "LINE_00") {
		t.Fatalf("expected LINE_00 in expanded view:\n%s", view)
	}
	if strings.Contains(view, "LINE_41") {
		t.Fatalf("expected LINE_41 to be cut off:\n%s", view)
	}
	if n := strings.Count(view, "\n") + 1; n > 20 {
		t.Fatalf("view has %d lines, want <= 20:\n%s", n, view)
	}
	if !strings.Contains(view, "of 42") {
		t.Fatalf("expected position hint:\n%s", view)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	view = StripAnsi(m.View())
	if strings.Contains(view, "LINE_00") {
		t.Fatalf("expected LINE_00 scrolled out after PgDn:\n%s", view)
	}

	for i := 0; i < 10; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	}
	view = StripAnsi(m.View())
	if !strings.Contains(view, "LINE_41") {
		t.Fatalf("expected LINE_41 at the end:\n%s", view)
	}
	if !strings.Contains(view, "Allow once") {
		t.Fatalf("options must stay visible:\n%s", view)
	}

	for i := 0; i < 10; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	}
	if !strings.Contains(StripAnsi(m.View()), "LINE_00") {
		t.Fatal("expected LINE_00 after PgUp to the top")
	}
}

func TestApprovalPagesDoNotSkipLines(t *testing.T) {
	m := newLongCommandModel(t)
	seen := map[int]bool{}
	mark := func() {
		for i := 0; i < 42; i++ {
			if strings.Contains(StripAnsi(m.View()), fmt.Sprintf("LINE_%02d", i)) {
				seen[i] = true
			}
		}
	}
	mark()
	for i := 0; i < 30; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		mark()
	}
	for i := 0; i < 42; i++ {
		if !seen[i] {
			t.Errorf("LINE_%02d was never visible while paging down", i)
		}
	}
}

func TestApprovalTinyTerminalStillCapsHeight(t *testing.T) {
	m := newLongCommandModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 6})
	if got := m.approvalMaxHeight(); got < 1 {
		t.Fatalf("approvalMaxHeight = %d, want >= 1", got)
	}
}

func TestSudoExpandedLongCommandIsCappedAndScrolls(t *testing.T) {
	m := New(nil, "")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	var lines []string
	for i := 0; i < 42; i++ {
		lines = append(lines, fmt.Sprintf("LINE_%02d", i))
	}
	sendPasswordOnlyReq(m, "main", "sudo true\n"+strings.Join(lines, "\n"))
	pressKey(m, tea.KeyTab)

	view := StripAnsi(m.View())
	if !strings.Contains(view, "LINE_00") || strings.Contains(view, "LINE_41") {
		t.Fatalf("expected a capped window from the top:\n%s", view)
	}
	if n := strings.Count(view, "\n") + 1; n > 20 {
		t.Fatalf("view has %d lines, want <= 20:\n%s", n, view)
	}
	if !strings.Contains(view, "of 43") || !strings.Contains(view, "Password:") {
		t.Fatalf("expected position hint and password field:\n%s", view)
	}

	for i := 0; i < 10; i++ {
		pressKey(m, tea.KeyPgDown)
	}
	view = StripAnsi(m.View())
	if !strings.Contains(view, "LINE_41") || strings.Contains(view, "LINE_00") {
		t.Fatalf("expected the end of the command after PgDn:\n%s", view)
	}
	if m.activeApproval.passwordInput.Value() != "" {
		t.Error("scroll keys must not type into the password field")
	}
}
