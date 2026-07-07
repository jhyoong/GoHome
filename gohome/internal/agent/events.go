package agent

import (
	"context"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// EventKind identifies the type of an agent event emitted to the Frontend.
type EventKind string

const (
	EventTokenDelta     EventKind = "token_delta"
	EventToolCallDone   EventKind = "tool_call_done"
	EventToolResult     EventKind = "tool_result"
	EventUsageUpdated   EventKind = "usage_updated"
	EventTurnDone       EventKind = "turn_done"
	EventSessionStarted EventKind = "session_started"
	EventSessionEnded   EventKind = "session_ended"
	EventSessionSwapped EventKind = "session_swapped"
	EventError          EventKind = "error"
	EventThinkingDelta  EventKind = "thinking_delta"
	EventThinkingDone   EventKind = "thinking_done"
)

// ToolResult carries the result of a single tool execution.
type ToolResult struct {
	ToolUseID string        `json:"toolUseID"`
	Content   string        `json:"content"`
	IsError   bool          `json:"isError"`
	Duration  time.Duration `json:"duration,omitempty"`
}

// TurnStats holds per-turn performance metrics.
type TurnStats struct {
	OutputTokens     int           `json:"outputTokens"`
	InputTokens      int           `json:"inputTokens"`
	CacheReadTokens  int           `json:"cacheReadTokens"`
	CacheWriteTokens int           `json:"cacheWriteTokens"`
	Elapsed          time.Duration `json:"elapsed"`
}

// Event is the unit the agent sends to its Frontend.
type Event struct {
	Kind          EventKind     `json:"kind"`
	SessionID     string        `json:"sessionID"`
	TextDelta     string        `json:"textDelta,omitempty"`
	ToolCallID    string        `json:"toolCallID,omitempty"`
	ToolName      string        `json:"toolName,omitempty"`
	InputJSON     string        `json:"inputJSON,omitempty"`
	Result        *ToolResult   `json:"result,omitempty"`
	Usage         *common.Usage `json:"usage,omitempty"`
	StopReason    string        `json:"stopReason,omitempty"`
	EndReason     string        `json:"endReason,omitempty"` // "done" or "cancelled" (EventSessionEnded only)
	Err           error         `json:"-"`
	ErrMessage    string        `json:"errMessage,omitempty"`
	ThinkingDelta string        `json:"thinkingDelta,omitempty"`
	TurnStats     *TurnStats    `json:"turnStats,omitempty"`
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
	AwaitUserInput(ctx context.Context) (string, error)
}
