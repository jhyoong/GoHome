package tui

import (
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

func TestHistoryToTimeline_UserMessage(t *testing.T) {
	msgs := []common.Message{
		{Role: common.RoleUser, Content: []common.Block{
			{Kind: common.BlockText, Text: "hello world"},
		}},
	}
	got := historyToTimeline(msgs)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Kind != "user" || got[0].Text != "hello world" {
		t.Errorf("got %+v", got[0])
	}
}

func TestHistoryToTimeline_AssistantTextAndThinking(t *testing.T) {
	msgs := []common.Message{
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockThinking, Text: "let me think"},
			{Kind: common.BlockText, Text: "here is the answer"},
		}},
	}
	got := historyToTimeline(msgs)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Kind != "thinking" || got[0].Text != "let me think" {
		t.Errorf("thinking entry: %+v", got[0])
	}
	if got[1].Kind != "assistant" || got[1].Text != "here is the answer" {
		t.Errorf("assistant entry: %+v", got[1])
	}
}

func TestHistoryToTimeline_ToolUseAndResult(t *testing.T) {
	msgs := []common.Message{
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockToolUse, ToolName: "bash", ToolUseID: "t1", InputJSON: `{"cmd":"ls"}`},
		}},
		{Role: common.RoleTool, Content: []common.Block{
			{Kind: common.BlockToolResult, ToolUseID: "t1", ResultText: "file.go", IsError: false},
		}},
	}
	got := historyToTimeline(msgs)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (tool use + result merged)", len(got))
	}
	if got[0].Kind != "tool" || got[0].ToolName != "bash" {
		t.Errorf("tool entry: %+v", got[0])
	}
	if got[0].Text != `{"cmd":"ls"}` {
		t.Errorf("tool input: %q", got[0].Text)
	}
	if got[0].ToolResult != "file.go" {
		t.Errorf("tool result: %q", got[0].ToolResult)
	}
	if got[0].Status != "success" {
		t.Errorf("tool status: %q", got[0].Status)
	}
}

func TestHistoryToTimeline_ToolError(t *testing.T) {
	msgs := []common.Message{
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockToolUse, ToolName: "bash", ToolUseID: "t2", InputJSON: `{"cmd":"fail"}`},
		}},
		{Role: common.RoleTool, Content: []common.Block{
			{Kind: common.BlockToolResult, ToolUseID: "t2", ResultText: "command not found", IsError: true},
		}},
	}
	got := historyToTimeline(msgs)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Status != "error" {
		t.Errorf("status = %q, want error", got[0].Status)
	}
}

func TestHistoryToTimeline_MultipleUserBlocks(t *testing.T) {
	msgs := []common.Message{
		{Role: common.RoleUser, Content: []common.Block{
			{Kind: common.BlockText, Text: "line one"},
			{Kind: common.BlockText, Text: "line two"},
		}},
	}
	got := historyToTimeline(msgs)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Text != "line one\nline two" {
		t.Errorf("text = %q", got[0].Text)
	}
}

func TestHistoryToTimeline_Empty(t *testing.T) {
	got := historyToTimeline(nil)
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}

func TestHistoryToTimeline_FullConversation(t *testing.T) {
	msgs := []common.Message{
		{Role: common.RoleUser, Content: []common.Block{
			{Kind: common.BlockText, Text: "fix the bug"},
		}},
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockThinking, Text: "analyzing"},
			{Kind: common.BlockText, Text: "I see the issue"},
			{Kind: common.BlockToolUse, ToolName: "edit", ToolUseID: "t1", InputJSON: `{"file":"main.go"}`},
		}},
		{Role: common.RoleTool, Content: []common.Block{
			{Kind: common.BlockToolResult, ToolUseID: "t1", ResultText: "ok"},
		}},
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockText, Text: "Fixed it"},
		}},
	}
	got := historyToTimeline(msgs)
	// Expected: user, thinking, assistant, tool(merged), assistant
	if len(got) != 5 {
		t.Fatalf("len = %d, want 5", len(got))
	}
	kinds := make([]string, len(got))
	for i, e := range got {
		kinds[i] = e.Kind
	}
	want := []string{"user", "thinking", "assistant", "tool", "assistant"}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("entry[%d].Kind = %q, want %q", i, kinds[i], want[i])
		}
	}
	if got[3].ToolResult != "ok" {
		t.Errorf("tool result not merged: %q", got[3].ToolResult)
	}
}
