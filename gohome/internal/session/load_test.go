package session

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")
	startedAt := time.Now().UTC().Add(-time.Hour)

	w, err := OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}

	// SessionStart
	w.Emit(SessionStart{
		ID:        "sess-load",
		ParentID:  "parent-1",
		CWD:       "/tmp/proj",
		Model:     "gpt-4o",
		Endpoint:  "https://api.example.com",
		Depth:     1,
		StartedAt: startedAt,
	})

	// UserMessage
	w.Emit(UserMessage{
		Content: []common.Block{
			{Kind: common.BlockText, Text: "Hello agent"},
		},
	})

	// AssistantMessage with a tool_use block
	w.Emit(AssistantMessage{
		Content: []common.Block{
			{Kind: common.BlockText, Text: "I will call a tool"},
			{Kind: common.BlockToolUse, ToolUseID: "tu1", ToolName: "bash", InputJSON: `{"cmd":"ls"}`},
		},
		StopReason: "tool_use",
		Usage:      &common.Usage{InputTokens: 10, OutputTokens: 5},
	})

	// ToolResult
	w.Emit(ToolResult{
		ToolUseID: "tu1",
		Content:   "file1.go\nfile2.go",
		IsError:   false,
	})

	// Approval (should be ignored for history)
	w.Emit(Approval{ToolUseID: "tu1", Outcome: "allow"})

	// SessionEnd (should be ignored for history)
	w.Emit(SessionEnd{Reason: "done"})

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	sess, history, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Check session metadata
	if sess.ID != "sess-load" {
		t.Errorf("sess.ID = %q, want %q", sess.ID, "sess-load")
	}
	if sess.ParentID != "parent-1" {
		t.Errorf("sess.ParentID = %q, want %q", sess.ParentID, "parent-1")
	}
	if sess.CWD() != "/tmp/proj" {
		t.Errorf("sess.CWD() = %q, want %q", sess.CWD(), "/tmp/proj")
	}
	if sess.Model != "gpt-4o" {
		t.Errorf("sess.Model = %q, want %q", sess.Model, "gpt-4o")
	}
	if sess.Endpoint != "https://api.example.com" {
		t.Errorf("sess.Endpoint = %q, want %q", sess.Endpoint, "https://api.example.com")
	}
	if sess.Depth != 1 {
		t.Errorf("sess.Depth = %d, want 1", sess.Depth)
	}
	if sess.StartedAt.IsZero() {
		t.Error("sess.StartedAt is zero")
	}

	// History should have exactly 3 turns: user, assistant, tool_result
	if len(history) != 3 {
		t.Fatalf("len(history) = %d, want 3", len(history))
	}

	// Turn 0: user message
	if history[0].Role != common.RoleUser {
		t.Errorf("history[0].Role = %q, want %q", history[0].Role, common.RoleUser)
	}
	if len(history[0].Content) != 1 || history[0].Content[0].Kind != common.BlockText {
		t.Errorf("history[0].Content unexpected: %+v", history[0].Content)
	}
	if history[0].Content[0].Text != "Hello agent" {
		t.Errorf("history[0].Content[0].Text = %q, want %q", history[0].Content[0].Text, "Hello agent")
	}

	// Turn 1: assistant message (with tool_use block)
	if history[1].Role != common.RoleAssistant {
		t.Errorf("history[1].Role = %q, want %q", history[1].Role, common.RoleAssistant)
	}
	if len(history[1].Content) != 2 {
		t.Fatalf("history[1].Content len = %d, want 2", len(history[1].Content))
	}
	if history[1].Content[1].Kind != common.BlockToolUse {
		t.Errorf("history[1].Content[1].Kind = %q, want tool_use", history[1].Content[1].Kind)
	}
	if history[1].Content[1].ToolUseID != "tu1" {
		t.Errorf("history[1].Content[1].ToolUseID = %q, want %q", history[1].Content[1].ToolUseID, "tu1")
	}

	// Turn 2: tool result
	if history[2].Role != common.RoleTool {
		t.Errorf("history[2].Role = %q, want %q", history[2].Role, common.RoleTool)
	}
	if len(history[2].Content) != 1 {
		t.Fatalf("history[2].Content len = %d, want 1", len(history[2].Content))
	}
	toolBlk := history[2].Content[0]
	if toolBlk.Kind != common.BlockToolResult {
		t.Errorf("toolBlk.Kind = %q, want tool_result", toolBlk.Kind)
	}
	if toolBlk.ToolUseID != "tu1" {
		t.Errorf("toolBlk.ToolUseID = %q, want %q", toolBlk.ToolUseID, "tu1")
	}
	if toolBlk.ResultText != "file1.go\nfile2.go" {
		t.Errorf("toolBlk.ResultText = %q, want %q", toolBlk.ResultText, "file1.go\nfile2.go")
	}
	if toolBlk.IsError {
		t.Error("toolBlk.IsError = true, want false")
	}

	// Session.History should also be set
	if len(sess.History) != 3 {
		t.Errorf("sess.History len = %d, want 3", len(sess.History))
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, _, err := Load("/nonexistent/path/session.jsonl")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}
