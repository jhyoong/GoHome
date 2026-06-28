package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
)

func TestEventSessionSwapped_PromotesApproval(t *testing.T) {
	m := New("sess-a")
	// Initialize with a window size so the model is usable.
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Add a pending approval for sess-b.
	m.handleApprovalReq(ApprovalReqMsg{
		Req: guard.ApprovalRequest{SessionID: "sess-b", Tool: "bash"},
		Resolve: func(dec guard.ApprovalDecision) {},
	})

	// sess-b should have a pending approval (not active, since focus is on sess-a).
	if _, ok := m.pendingApprovals["sess-b"]; !ok {
		t.Fatal("expected pending approval for sess-b")
	}

	// Simulate EventSessionSwapped to sess-b.
	m.handleAgentEvent(AgentEventMsg{
		SessionID: "sess-b",
		Ev: agent.Event{Kind: agent.EventSessionSwapped, SessionID: "sess-b"},
	})

	// Now sess-b is focused, its pending approval should be promoted to active.
	if m.activeApproval == nil {
		t.Error("expected pending approval for sess-b to be promoted to active")
	}
	if _, ok := m.pendingApprovals["sess-b"]; ok {
		t.Error("sess-b should no longer have a pending approval")
	}
}

func TestEventSessionSwapped_DemotesActiveApproval(t *testing.T) {
	m := New("sess-a")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Add an active approval for sess-a (focused session).
	m.handleApprovalReq(ApprovalReqMsg{
		Req: guard.ApprovalRequest{SessionID: "sess-a", Tool: "bash"},
		Resolve: func(dec guard.ApprovalDecision) {},
	})

	// It should be active since sess-a is focused.
	if m.activeApproval == nil {
		t.Fatal("expected active approval for sess-a")
	}

	// Simulate EventSessionSwapped to sess-b.
	m.handleAgentEvent(AgentEventMsg{
		SessionID: "sess-b",
		Ev: agent.Event{Kind: agent.EventSessionSwapped, SessionID: "sess-b"},
	})

	// The old active approval for sess-a should be demoted to pending.
	if _, ok := m.pendingApprovals["sess-a"]; !ok {
		t.Error("expected sess-a approval to be demoted to pending")
	}
	// No active approval should remain (sess-b has no pending approval).
	if m.activeApproval != nil {
		t.Error("expected no active approval after swapping to sess-b with no pending")
	}
}
