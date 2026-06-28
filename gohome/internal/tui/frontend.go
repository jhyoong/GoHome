package tui

import (
	"github.com/jhyoong/GoHome/gohome/internal/agent"
)

// AgentEventMsg wraps an agent.Event for delivery to the Bubble Tea update loop.
// It is exported so tests can send it directly via tm.Send.
type AgentEventMsg struct {
	SessionID string
	Ev        agent.Event
}

// ExternalEditorMsg is sent when the external editor process exits.
type ExternalEditorMsg struct {
	Content string
	Err     error
}
