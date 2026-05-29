package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
)

// AgentEventMsg wraps an agent.Event for delivery to the Bubble Tea update loop.
// It is exported so tests can send it directly via tm.Send.
type AgentEventMsg struct {
	SessionID string
	Ev        agent.Event
}

// agentEventMsg is an internal alias kept so existing code compiles.
// We use AgentEventMsg everywhere; this line ensures the switch in Update works.
type agentEventMsg = AgentEventMsg

// Frontend bridges the agent layer and the Bubble Tea program.
// It implements agent.Frontend.
type Frontend struct {
	prog  *tea.Program
	input chan string
}

// NewFrontend creates a Frontend ready to be wired to a tea.Program.
func NewFrontend() *Frontend {
	return &Frontend{
		input: make(chan string),
	}
}

// SetProgram wires the tea.Program so that Emit can send messages to it.
func (f *Frontend) SetProgram(p *tea.Program) {
	f.prog = p
}

// Emit implements agent.Frontend. It is safe to call concurrently and does
// not block the caller: Send is non-blocking when the program is ready.
func (f *Frontend) Emit(sessionID string, ev agent.Event) {
	if f.prog != nil {
		f.prog.Send(AgentEventMsg{SessionID: sessionID, Ev: ev})
	}
}

// RequestApproval implements agent.Frontend.
// TODO(11.9): real approval overlay.
func (f *Frontend) RequestApproval(_ context.Context, _ guard.ApprovalRequest) (guard.ApprovalDecision, error) {
	return guard.ApprovalDecision{Outcome: guard.Deny}, nil
}

// AwaitUserInput implements agent.Frontend. It blocks until the user submits
// text via the TUI input or ctx is cancelled.
func (f *Frontend) AwaitUserInput(ctx context.Context, _ string) (string, error) {
	select {
	case s := <-f.input:
		return s, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
