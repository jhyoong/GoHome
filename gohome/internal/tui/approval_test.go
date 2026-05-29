package tui_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
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

// --- Task 11.10: editable allow-always pattern ---

func TestApprovalAllowAlwaysDefaultPattern(t *testing.T) {
	m := tui.New(nil)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	msg, ch := makeApprovalReq("main", "bash", "^ls", json.RawMessage(`{"command":"ls"}`))
	tm.Send(msg)

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Approve"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Press '2' without editing -> AllowAlways with the original pattern.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})

	select {
	case dec := <-ch:
		if dec.Outcome != guard.AllowAlways {
			t.Fatalf("expected AllowAlways, got %q", dec.Outcome)
		}
		if dec.SavedPattern != "^ls" {
			t.Fatalf("expected SavedPattern %q, got %q", "^ls", dec.SavedPattern)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for AllowAlways decision")
	}
}

func TestApprovalEditPatternThenAllowAlways(t *testing.T) {
	m := tui.New(nil)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	msg, ch := makeApprovalReq("main", "bash", "^ls", json.RawMessage(`{"command":"ls"}`))
	tm.Send(msg)

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Approve"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Press 'e' to enter edit mode.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	// Type extra text.
	tm.Type(" -- extra")
	// Confirm edit with Enter.
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	// Press '2' to allow always.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})

	select {
	case dec := <-ch:
		if dec.Outcome != guard.AllowAlways {
			t.Fatalf("expected AllowAlways, got %q", dec.Outcome)
		}
		want := "^ls -- extra"
		if dec.SavedPattern != want {
			t.Fatalf("expected SavedPattern %q, got %q", want, dec.SavedPattern)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for AllowAlways decision")
	}
}

func TestApprovalEditPatternEscReverts(t *testing.T) {
	m := tui.New(nil)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	msg, ch := makeApprovalReq("main", "bash", "^ls", json.RawMessage(`{"command":"ls"}`))
	tm.Send(msg)

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Approve"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Enter edit mode, type something, then Esc to revert.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	tm.Type("DISCARDED")
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	// Now press '2' -> pattern should still be "^ls".
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})

	select {
	case dec := <-ch:
		if dec.Outcome != guard.AllowAlways {
			t.Fatalf("expected AllowAlways, got %q", dec.Outcome)
		}
		if dec.SavedPattern != "^ls" {
			t.Fatalf("expected reverted pattern %q, got %q", "^ls", dec.SavedPattern)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for AllowAlways decision")
	}
}

// --- Task 11.11: deny + steer ---

func TestApprovalDenySteer(t *testing.T) {
	m := tui.New(nil)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	msg, ch := makeApprovalReq("main", "bash", "^ls", json.RawMessage(`{"command":"ls"}`))
	tm.Send(msg)

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Approve"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Press '4' to enter steer mode.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	tm.Type("use rg instead")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case dec := <-ch:
		if dec.Outcome != guard.DenySteer {
			t.Fatalf("expected DenySteer, got %q", dec.Outcome)
		}
		if dec.SteerMessage != "use rg instead" {
			t.Fatalf("expected SteerMessage %q, got %q", "use rg instead", dec.SteerMessage)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for DenySteer decision")
	}
}

func TestApprovalDenySteerEscCancels(t *testing.T) {
	m := tui.New(nil)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	msg, ch := makeApprovalReq("main", "bash", "^ls", json.RawMessage(`{"command":"ls"}`))
	tm.Send(msg)

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("Approve"))
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Enter steer mode, then Esc -> should return to menu without resolving.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	tm.Type("cancel me")
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})

	// The overlay should still be active (no decision sent yet).
	// Now press '1' -> AllowOnce should be the actual decision.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})

	select {
	case dec := <-ch:
		if dec.Outcome != guard.AllowOnce {
			t.Fatalf("expected AllowOnce after Esc-from-steer, got %q", dec.Outcome)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for decision after Esc-from-steer")
	}
}

// --- Task 11.12: cross-session notification line ---

func TestCrossSessionNotificationLine(t *testing.T) {
	m := tui.New(nil)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	// Seed a second session "sub-1".
	tm.Send(tui.AgentEventMsg{
		SessionID: "sub-1",
		Ev: agent.Event{
			Kind:      agent.EventSessionStarted,
			SessionID: "sub-1",
		},
	})

	// Send an approval for "sub-1" while focused on "main".
	subMsg, _ := makeApprovalReq("sub-1", "bash", "^ls", json.RawMessage(`{"command":"ls"}`))
	tm.Send(subMsg)

	// The notification line must mention "sub-1" and "approval".
	// In the same frame, the normal textarea must be visible (no Approve overlay on "main").
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		hasNotif := bytes.Contains(out, []byte("sub-1")) && bytes.Contains(out, []byte("approval"))
		noOverlay := !bytes.Contains(out, []byte("Approve tool call"))
		return hasNotif && noOverlay
	}, teatest.WithDuration(2*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}
