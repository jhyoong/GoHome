package agent

import (
	"context"

	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// EventKind identifies the type of an agent event emitted to the Frontend.
type EventKind string

const (
	EventTokenDelta     EventKind = "token_delta"
	EventToolCallStart  EventKind = "tool_call_start"
	EventToolCallDone   EventKind = "tool_call_done"
	EventToolResult     EventKind = "tool_result"
	EventUsageUpdated   EventKind = "usage_updated"
	EventTurnDone       EventKind = "turn_done"
	EventSessionStarted EventKind = "session_started"
	EventSessionEnded   EventKind = "session_ended"
	EventError          EventKind = "error"
)

// ToolResult carries the result of a single tool execution.
type ToolResult struct {
	ToolUseID string
	Content   string
	IsError   bool
}

// Event is the unit the agent sends to its Frontend.
type Event struct {
	Kind       EventKind
	SessionID  string
	TextDelta  string
	ToolCallID string
	ToolName   string
	InputJSON  string
	Result     *ToolResult
	Usage      *common.Usage
	StopReason string
	Err        error
}

// Frontend is implemented by the TUI (or any other consumer) and receives
// events from the agent. The agent package must not import the tui package;
// instead, the TUI implements this interface and is injected.
type Frontend interface {
	// Emit sends an agent event to the frontend. It must be safe to call
	// concurrently and must not block the agent goroutine for long.
	Emit(sessionID string, ev Event)

	// RequestApproval asks the user whether a tool call should be permitted.
	// It blocks until the user responds or ctx is cancelled.
	RequestApproval(ctx context.Context, req guard.ApprovalRequest) (guard.ApprovalDecision, error)

	// AwaitUserInput blocks until the user submits a follow-up prompt or ctx
	// is cancelled.
	AwaitUserInput(ctx context.Context, sessionID string) (string, error)
}
