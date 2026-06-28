package agent

import (
	"context"
	"encoding/json"
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

// TestEvent_JSONRoundTrip verifies that Event can survive a JSON round-trip
// with the correct field names (camelCase, not Go-exported PascalCase).
func TestEvent_JSONRoundTrip(t *testing.T) {
	ev := Event{
		Kind:      EventTokenDelta,
		SessionID: "s1",
		TextDelta: "hello",
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Verify the JSON uses camelCase keys, not PascalCase.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}
	if _, ok := raw["kind"]; !ok {
		t.Errorf("expected camelCase key \"kind\" in JSON, got keys: %v", raw)
	}
	if _, ok := raw["sessionID"]; !ok {
		t.Errorf("expected camelCase key \"sessionID\" in JSON, got keys: %v", raw)
	}

	// Verify omitempty: fields with zero values should be absent.
	if _, ok := raw["toolName"]; ok {
		t.Errorf("expected toolName to be omitted when empty, got: %v", raw["toolName"])
	}

	var ev2 Event
	if err := json.Unmarshal(data, &ev2); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if ev2.Kind != EventTokenDelta {
		t.Errorf("Kind: got %v", ev2.Kind)
	}
	if ev2.TextDelta != "hello" {
		t.Errorf("TextDelta: got %q", ev2.TextDelta)
	}
	if ev2.SessionID != "s1" {
		t.Errorf("SessionID: got %q", ev2.SessionID)
	}
}

// TestEvent_JSONRoundTrip_WithToolResult verifies ToolResult serialization.
func TestEvent_JSONRoundTrip_WithToolResult(t *testing.T) {
	ev := Event{
		Kind:      EventToolResult,
		SessionID: "s2",
		Result: &ToolResult{
			ToolUseID: "tu1",
			Content:   "file contents",
			IsError:   true,
		},
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var ev2 Event
	if err := json.Unmarshal(data, &ev2); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if ev2.Result == nil {
		t.Fatal("Result is nil after round-trip")
	}
	if ev2.Result.ToolUseID != "tu1" {
		t.Errorf("ToolUseID: got %q", ev2.Result.ToolUseID)
	}
	if ev2.Result.Content != "file contents" {
		t.Errorf("Content: got %q", ev2.Result.Content)
	}
	if !ev2.Result.IsError {
		t.Errorf("IsError: got false, want true")
	}
}

// TestEvent_JSONRoundTrip_ErrMessage verifies that Err is excluded and
// ErrMessage is serialized.
func TestEvent_JSONRoundTrip_ErrMessage(t *testing.T) {
	ev := Event{
		Kind:       EventError,
		SessionID:  "s3",
		Err:        context.Canceled,
		ErrMessage: "context canceled",
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}
	// Err should be excluded (json:"-").
	if _, ok := raw["Err"]; ok {
		t.Errorf("Err field should be excluded from JSON")
	}
	if msg, ok := raw["errMessage"]; !ok || msg != "context canceled" {
		t.Errorf("errMessage: got %v", msg)
	}

	var ev2 Event
	if err := json.Unmarshal(data, &ev2); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if ev2.ErrMessage != "context canceled" {
		t.Errorf("ErrMessage: got %q", ev2.ErrMessage)
	}
	// Err should remain nil after unmarshal (it's excluded).
	if ev2.Err != nil {
		t.Errorf("Err should be nil after unmarshal, got %v", ev2.Err)
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
