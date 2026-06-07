package agent

import (
	"context"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// Compile-time check: Frontend interface is satisfied by fakeFrontend.
var _ Frontend = (*fakeRecorder)(nil)

// fakeRecorder is the shared test double used across all agent tests.
type fakeRecorder struct {
	events       []Event
	approval     guard.ApprovalDecision
	approvalErr  error
	userInput    string
	userInputErr error
}

func (f *fakeRecorder) Emit(_ string, ev Event) {
	f.events = append(f.events, ev)
}

func (f *fakeRecorder) RequestApproval(_ context.Context, _ guard.ApprovalRequest) (guard.ApprovalDecision, error) {
	return f.approval, f.approvalErr
}

func (f *fakeRecorder) AwaitUserInput(_ context.Context, _ string) (string, error) {
	return f.userInput, f.userInputErr
}

// TestEventKindConstants verifies the string values for each EventKind.
func TestEventKindConstants(t *testing.T) {
	cases := []struct {
		kind EventKind
		want string
	}{
		{EventTokenDelta, "token_delta"},
		{EventToolCallStart, "tool_call_start"},
		{EventToolCallDone, "tool_call_done"},
		{EventToolResult, "tool_result"},
		{EventUsageUpdated, "usage_updated"},
		{EventTurnDone, "turn_done"},
		{EventSessionStarted, "session_started"},
		{EventSessionEnded, "session_ended"},
		{EventError, "error"},
		{EventThinkingDelta, "thinking_delta"},
		{EventThinkingDone, "thinking_done"},
	}
	for _, tc := range cases {
		if string(tc.kind) != tc.want {
			t.Errorf("EventKind %v: got %q, want %q", tc.kind, string(tc.kind), tc.want)
		}
	}
}

// TestEventStruct verifies that Event carries the right fields.
func TestEventStruct(t *testing.T) {
	usage := &common.Usage{InputTokens: 1, OutputTokens: 2}
	result := &ToolResult{ToolUseID: "id1", Content: "ok", IsError: false}
	ev := Event{
		Kind:       EventTokenDelta,
		SessionID:  "sess1",
		TextDelta:  "hello",
		ToolCallID: "tc1",
		ToolName:   "read",
		InputJSON:  `{"path":"/tmp/x"}`,
		Result:     result,
		Usage:      usage,
		StopReason: "end_turn",
		Err:        nil,
	}
	if ev.Kind != EventTokenDelta {
		t.Errorf("Kind: got %v", ev.Kind)
	}
	if ev.Usage != usage {
		t.Errorf("Usage pointer mismatch")
	}
	if ev.Result != result {
		t.Errorf("Result pointer mismatch")
	}
}
