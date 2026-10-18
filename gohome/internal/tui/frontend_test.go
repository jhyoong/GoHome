package tui

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
)

// approvalSpy is a minimal tea.Model that reports each ApprovalReqMsg it gets.
type approvalSpy struct{ got chan ApprovalReqMsg }

func (s approvalSpy) Init() tea.Cmd { return nil }
func (s approvalSpy) View() string  { return "" }
func (s approvalSpy) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if req, ok := msg.(ApprovalReqMsg); ok {
		s.got <- req
	}
	return s, nil
}

// TestRequestApproval_CancelledCtxSendsNoPrompt verifies that a cancelled
// ctx returns Deny with ctx.Err() and never shows an approval prompt.
func TestRequestApproval_CancelledCtxSendsNoPrompt(t *testing.T) {
	spy := approvalSpy{got: make(chan ApprovalReqMsg, 1)}
	p := tea.NewProgram(spy, tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer())
	done := make(chan struct{})
	go func() {
		_, _ = p.Run()
		close(done)
	}()
	t.Cleanup(func() {
		p.Quit()
		<-done
	})

	f := NewFrontend()
	f.SetProgram(p)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dec, err := f.RequestApproval(ctx, guard.ApprovalRequest{SessionID: "main", Tool: "shell"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err: got %v, want context.Canceled", err)
	}
	if dec.Outcome != guard.Deny {
		t.Errorf("outcome: got %q, want Deny", dec.Outcome)
	}

	select {
	case req := <-spy.got:
		t.Errorf("approval prompt sent after cancel: %+v", req.Req)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestCancelClearsApprovals verifies that cancelling the run
// denies the active and queued approval prompts and clears both.
func TestCancelClearsApprovals(t *testing.T) {
	m := newSudoTestModel()
	active := make(chan guard.ApprovalDecision, 1)
	queued := make(chan guard.ApprovalDecision, 1)
	m.Update(ApprovalReqMsg{Req: guard.ApprovalRequest{SessionID: "main", Tool: "shell"}, Reply: active})
	m.Update(ApprovalReqMsg{Req: guard.ApprovalRequest{SessionID: "main", Tool: "write"}, Reply: queued})
	if m.activeApproval == nil || len(m.approvalQueue) != 1 {
		t.Fatalf("setup: active=%v queue=%d", m.activeApproval != nil, len(m.approvalQueue))
	}

	m.cancelRun()

	if m.activeApproval != nil {
		t.Error("activeApproval not cleared")
	}
	if len(m.approvalQueue) != 0 {
		t.Errorf("approvalQueue: got %d, want 0", len(m.approvalQueue))
	}
	if dec := requireReply(t, active); dec.Outcome != guard.Deny {
		t.Errorf("active reply: got %q, want Deny", dec.Outcome)
	}
	if dec := requireReply(t, queued); dec.Outcome != guard.Deny {
		t.Errorf("queued reply: got %q, want Deny", dec.Outcome)
	}
}

// TestSpinnerShowsWaitingForApproval verifies that the spinner says it is
// waiting while an approval prompt is open, and moves on once it is resolved.
func TestSpinnerShowsWaitingForApproval(t *testing.T) {
	m := newSudoTestModel()
	m.spinner.Start("Generating...")

	reply := make(chan guard.ApprovalDecision, 1)
	m.Update(ApprovalReqMsg{Req: guard.ApprovalRequest{SessionID: "main", Tool: "shell"}, Reply: reply})
	if m.spinner.message != "Waiting for approval..." {
		t.Errorf("spinner message with approval open = %q, want %q", m.spinner.message, "Waiting for approval...")
	}

	m.resolveApproval(guard.ApprovalDecision{Outcome: guard.AllowOnce})
	if m.spinner.message != "Processing..." {
		t.Errorf("spinner message after allow = %q, want %q", m.spinner.message, "Processing...")
	}
}
