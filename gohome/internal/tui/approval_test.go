package tui_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/tui"
)

// makeApprovalReq is a test helper that creates an ApprovalReqMsg for testing.
// It returns the message and the buffered reply channel.
func makeApprovalReq(sessionID, tool, suggestedPattern string, inputJSON json.RawMessage) (tui.ApprovalReqMsg, chan guard.ApprovalDecision) {
	ch := make(chan guard.ApprovalDecision, 1)
	msg := tui.ApprovalReqMsg{
		Req: guard.ApprovalRequest{
			SessionID:        sessionID,
			Tool:             tool,
			Input:            inputJSON,
			SuggestedPattern: suggestedPattern,
		},
		Reply: ch,
	}
	return msg, ch
}

// --- Task 11.9: approval prompt overlay basics ---

func TestApprovalOverlayShowsAndAllowOnce(t *testing.T) {
	m := tui.New(nil)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	msg, ch := makeApprovalReq("main", "bash", "^ls", json.RawMessage(`{"command":"ls"}`))
	tm.Send(msg)

	// Wait for the overlay to appear.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Approve")) && bytes.Contains(out, []byte("bash"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Press '1' -> AllowOnce.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})

	// The reply channel must receive AllowOnce without blocking.
	select {
	case dec := <-ch:
		if dec.Outcome != guard.AllowOnce {
			t.Fatalf("expected AllowOnce, got %q", dec.Outcome)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for AllowOnce decision")
	}
}

func TestApprovalOverlayEscDenies(t *testing.T) {
	m := tui.New(nil)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	msg, ch := makeApprovalReq("main", "bash", "^ls", json.RawMessage(`{"command":"ls"}`))
	tm.Send(msg)

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Approve"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})

	select {
	case dec := <-ch:
		if dec.Outcome != guard.Deny {
			t.Fatalf("expected Deny, got %q", dec.Outcome)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Deny decision")
	}
}

func TestApprovalOverlayKey3Denies(t *testing.T) {
	m := tui.New(nil)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	msg, ch := makeApprovalReq("main", "bash", "^ls", json.RawMessage(`{"command":"ls"}`))
	tm.Send(msg)

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Approve"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})

	select {
	case dec := <-ch:
		if dec.Outcome != guard.Deny {
			t.Fatalf("expected Deny, got %q", dec.Outcome)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Deny decision")
	}
}
