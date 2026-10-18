package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
)

// sendShellReq sends a shell approval request with the given suggested
// pattern and sudo flag.
func sendShellReq(m *Model, command, pattern string, sudo bool) chan guard.ApprovalDecision {
	ch := make(chan guard.ApprovalDecision, 1)
	input, _ := json.Marshal(map[string]string{"command": command})
	m.Update(ApprovalReqMsg{
		Req: guard.ApprovalRequest{
			SessionID:         "main",
			Tool:              "shell",
			Input:             input,
			SuggestedPattern:  pattern,
			NeedsSudoPassword: sudo,
		},
		Reply: ch,
	})
	return ch
}

func renderActiveApproval(m *Model) string {
	return renderApprovalOverlay(m.activeApproval, 80, 0, "main")
}

// With no suggested pattern, the user can still write one, so the edit hint
// is shown.
func TestApproval_EmptyPatternShowsEditHint(t *testing.T) {
	m := newSudoTestModel()
	sendShellReq(m, "git status; echo x", "", false)

	out := renderActiveApproval(m)
	if !strings.Contains(out, "[2] Allow always  (e edit)") {
		t.Errorf("edit hint missing:\n%s", out)
	}
}

// The pattern error from a failed Allow always is cleared when the user
// moves to another option.
func TestApproval_ArrowClearsPatternError(t *testing.T) {
	m := newSudoTestModel()
	ch := sendShellReq(m, "git status; echo x", "", false)

	typeRunes(m, "2")
	requireNoReply(t, ch)
	if m.activeApproval.patternErr == "" {
		t.Fatal("expected a pattern error after Allow always with no pattern")
	}

	pressKey(m, tea.KeyDown)
	if m.activeApproval.patternErr != "" {
		t.Errorf("patternErr after Down: got %q, want empty", m.activeApproval.patternErr)
	}
	typeRunes(m, "2")
	pressKey(m, tea.KeyUp)
	if m.activeApproval.patternErr != "" {
		t.Errorf("patternErr after Up: got %q, want empty", m.activeApproval.patternErr)
	}
}

// A sudo command with no suggested pattern cannot be edited, so Allow always
// is marked as not available.
func TestApproval_SudoEmptyPatternNotAvailable(t *testing.T) {
	m := newSudoTestModel()
	ch := sendShellReq(m, "sudo ls; id", "", true)

	out := renderActiveApproval(m)
	if !strings.Contains(out, "[2] Allow always  (not available for this command)") {
		t.Errorf("not-available label missing:\n%s", out)
	}
	if strings.Contains(out, "(e edit)") {
		t.Errorf("sudo prompt should not offer editing:\n%s", out)
	}

	typeRunes(m, "2")
	requireNoReply(t, ch)
	if m.activeApproval.sudoStage {
		t.Error("Allow always opened the password stage")
	}
	const want = "Allow always is not available for this command"
	if m.activeApproval.patternErr != want {
		t.Errorf("patternErr: got %q, want %q", m.activeApproval.patternErr, want)
	}
}

// Editing is disabled for sudo, so a sudo prompt with a pattern shows the
// pattern without the edit hint.
func TestApproval_SudoPatternHasNoEditHint(t *testing.T) {
	m := newSudoTestModel()
	sendShellReq(m, "sudo true", `^sudo true$`, true)

	out := renderActiveApproval(m)
	if !strings.Contains(out, "[2] Allow always  ^sudo true$") {
		t.Errorf("pattern missing:\n%s", out)
	}
	if strings.Contains(out, "(e edit)") {
		t.Errorf("sudo prompt should not offer editing:\n%s", out)
	}
}
