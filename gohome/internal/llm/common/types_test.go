package common_test

import (
	"encoding/json"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

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
