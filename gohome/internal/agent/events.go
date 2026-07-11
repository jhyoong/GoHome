package agent

import (
	"context"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

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
	EventSending        EventKind = "sending"
	EventThinkingDelta  EventKind = "thinking_delta"
	EventThinkingDone   EventKind = "thinking_done"
	EventToolDenied     EventKind = "tool_denied"
)

type ToolResult struct {
	ToolUseID string        `json:"toolUseID"`
	Content   string        `json:"content"`
	IsError   bool          `json:"isError"`
	Duration  time.Duration `json:"duration,omitempty"`
}

type TurnStats struct {
	OutputTokens     int           `json:"outputTokens"`
	InputTokens      int           `json:"inputTokens"`
	CacheReadTokens  int           `json:"cacheReadTokens"`
	CacheWriteTokens int           `json:"cacheWriteTokens"`
	Elapsed          time.Duration `json:"elapsed"`
}

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
	EndReason     string        `json:"endReason,omitempty"`
	Err           error         `json:"-"`
	ErrMessage    string        `json:"errMessage,omitempty"`
	ThinkingDelta string        `json:"thinkingDelta,omitempty"`
	TurnStats     *TurnStats    `json:"turnStats,omitempty"`
}

type Frontend interface {
	Emit(sessionID string, ev Event)
	RequestApproval(ctx context.Context, req guard.ApprovalRequest) (guard.ApprovalDecision, error)
	AwaitUserInput(ctx context.Context) (string, error)
}
