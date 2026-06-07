package common_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// fakeClient is a local stub that satisfies common.Client.
type fakeClient struct{}

func (fakeClient) Stream(_ context.Context, _ common.Request) (<-chan common.StreamEvent, error) {
	ch := make(chan common.StreamEvent)
	close(ch)
	return ch, nil
}

func TestRequest_FieldsAndClientInterface(t *testing.T) {
	schema := json.RawMessage(`{"type":"object"}`)
	req := common.Request{
		Model:  "gpt-4o",
		System: "you are helpful",
		Messages: []common.Message{
			{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}},
		},
		Tools: []common.ToolDef{
			{Name: "search", Description: "web search", InputSchema: schema},
		},
		MaxTokens: 512,
	}

	if req.Model != "gpt-4o" {
		t.Errorf("model: want gpt-4o, got %q", req.Model)
	}
	if len(req.Tools) != 1 {
		t.Fatalf("tools length: want 1, got %d", len(req.Tools))
	}
	if req.Tools[0].Name != "search" {
		t.Errorf("tool name: want search, got %q", req.Tools[0].Name)
	}
	if string(req.Tools[0].InputSchema) != `{"type":"object"}` {
		t.Errorf("input schema: want raw json, got %s", req.Tools[0].InputSchema)
	}
	if req.MaxTokens != 512 {
		t.Errorf("maxTokens: want 512, got %d", req.MaxTokens)
	}

	// Confirm fakeClient satisfies the interface at compile time.
	var _ common.Client = fakeClient{}
}

func TestStreamEvent_KindAndNilUsage(t *testing.T) {
	ev := common.StreamEvent{Kind: common.EventTextDelta, TextDelta: "x"}
	if ev.Kind != common.EventTextDelta {
		t.Errorf("kind: want %q, got %q", common.EventTextDelta, ev.Kind)
	}

	var zero common.StreamEvent
	if zero.Usage != nil {
		t.Errorf("nil usage: want nil, got %+v", zero.Usage)
	}
}

func TestThinkingBlockKind(t *testing.T) {
	if string(common.BlockThinking) != "thinking" {
		t.Errorf("BlockThinking: got %q, want %q", common.BlockThinking, "thinking")
	}
}

func TestThinkingEventKinds(t *testing.T) {
	if string(common.EventThinkingDelta) != "thinking_delta" {
		t.Errorf("EventThinkingDelta: got %q", common.EventThinkingDelta)
	}
	if string(common.EventThinkingDone) != "thinking_done" {
		t.Errorf("EventThinkingDone: got %q", common.EventThinkingDone)
	}
}

func TestStreamEvent_ThinkingDelta(t *testing.T) {
	ev := common.StreamEvent{Kind: common.EventThinkingDelta, ThinkingDelta: "reasoning about X"}
	if ev.ThinkingDelta != "reasoning about X" {
		t.Errorf("ThinkingDelta: got %q", ev.ThinkingDelta)
	}
}

func TestMessage_JSONRoundtrip(t *testing.T) {
	original := common.Message{
		Role: common.RoleUser,
		Content: []common.Block{
			{Kind: common.BlockText, Text: "hello"},
			{Kind: common.BlockToolUse, ToolUseID: "tu1", ToolName: "search", InputJSON: `{"q":"go"}`},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got common.Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Role != common.RoleUser {
		t.Errorf("role: want %q, got %q", common.RoleUser, got.Role)
	}
	if len(got.Content) != 2 {
		t.Fatalf("content length: want 2, got %d", len(got.Content))
	}
	if got.Content[0].Kind != common.BlockText || got.Content[0].Text != "hello" {
		t.Errorf("block[0]: want text block with 'hello', got %+v", got.Content[0])
	}
	if got.Content[1].Kind != common.BlockToolUse || got.Content[1].ToolName != "search" {
		t.Errorf("block[1]: want tool_use block with name 'search', got %+v", got.Content[1])
	}
}
