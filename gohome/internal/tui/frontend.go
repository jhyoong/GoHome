package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
)

// Compile-time assertions: Frontend must satisfy both agent.Frontend and
// guard.Frontend. If either interface changes incompatibly, this file will
// fail to compile, surfacing the mismatch immediately.
var (
	_ agent.Frontend = (*Frontend)(nil)
	_ guard.Frontend = (*Frontend)(nil)
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

// ExternalEditorMsg is sent when the external editor process exits.
type ExternalEditorMsg struct {
	Content string
	Err     error
}

// externalEditorMsg is an internal alias.
type externalEditorMsg = ExternalEditorMsg

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

// InputCh returns the channel that delivers user-submitted text. The Model
// sends on this channel when the user presses Enter; AwaitUserInput reads it.
func (f *Frontend) InputCh() chan string {
	return f.input
}

// Emit implements agent.Frontend. It is safe to call concurrently and does
// not block the caller: Send is non-blocking when the program is ready.
func (f *Frontend) Emit(sessionID string, ev agent.Event) {
	if f.prog != nil {
		f.prog.Send(AgentEventMsg{SessionID: sessionID, Ev: ev})
	}
}

// RequestApproval implements agent.Frontend and guard.Frontend.
// It sends an approvalReqMsg to the Bubble Tea loop and blocks until the UI
// resolves the prompt or ctx is cancelled.
// If no program has been wired (f.prog == nil) it returns Deny immediately
// rather than blocking until context cancellation.
func (f *Frontend) RequestApproval(ctx context.Context, req guard.ApprovalRequest) (guard.ApprovalDecision, error) {
	if f.prog == nil {
		return guard.ApprovalDecision{Outcome: guard.Deny}, fmt.Errorf("tui: no program wired")
	}
	reply := make(chan guard.ApprovalDecision, 1)
	f.prog.Send(ApprovalReqMsg{Req: req, Reply: reply})
	select {
	case dec := <-reply:
		return dec, nil
	case <-ctx.Done():
		return guard.ApprovalDecision{Outcome: guard.Deny}, ctx.Err()
	}
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
