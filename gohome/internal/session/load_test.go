package session

import (
	"path/filepath"
	"strings"
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
		ID:          "sess-load",
		ParentID:    "parent-1",
		CWD:         "/tmp/proj",
		Model:       "gpt-4o",
		ModelConfig: "https://api.example.com",
		Depth:       1,
		StartedAt:   startedAt,
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
			{Kind: common.BlockToolUse, ToolUseID: "tu1", ToolName: "shell", InputJSON: `{"cmd":"ls"}`},
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
	if sess.ModelConfig != "https://api.example.com" {
		t.Errorf("sess.ModelConfig = %q, want %q", sess.ModelConfig, "https://api.example.com")
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

func TestLoad_WithCompaction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	w, err := OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}

	w.Emit(SessionStart{ID: "sess-compact", CWD: "/tmp", Model: "m"})
	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "old message 1"}}})
	w.Emit(AssistantMessage{Content: []common.Block{{Kind: common.BlockText, Text: "old reply 1"}}})
	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "old message 2"}}})
	w.Emit(AssistantMessage{Content: []common.Block{{Kind: common.BlockText, Text: "old reply 2"}}})

	w.Emit(Compaction{BeforeTokens: 50000, AfterTokens: 10000, Summary: "Summary of conversation so far."})

	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "new message"}}})
	w.Emit(AssistantMessage{Content: []common.Block{{Kind: common.BlockText, Text: "new reply"}}})

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	sess, history, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if sess.ID != "sess-compact" {
		t.Errorf("sess.ID = %q", sess.ID)
	}

	// History should be: summary + new user + new assistant = 3
	if len(history) != 3 {
		t.Fatalf("len(history) = %d, want 3", len(history))
	}
	if history[0].Role != common.RoleUser {
		t.Errorf("history[0].Role = %q, want user", history[0].Role)
	}
	if history[0].Content[0].Text != "[Auto-compact summary]\n\nSummary of conversation so far." {
		t.Errorf("history[0] text = %q", history[0].Content[0].Text)
	}
	if history[1].Content[0].Text != "new message" {
		t.Errorf("history[1] text = %q", history[1].Content[0].Text)
	}
	if history[2].Content[0].Text != "new reply" {
		t.Errorf("history[2] text = %q", history[2].Content[0].Text)
	}
}

func TestLoad_MultipleCompactions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	w, err := OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}

	w.Emit(SessionStart{ID: "sess-multi", CWD: "/tmp", Model: "m"})
	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "old"}}})
	w.Emit(Compaction{BeforeTokens: 50000, AfterTokens: 10000, Summary: "first summary"})
	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "middle"}}})
	w.Emit(Compaction{BeforeTokens: 40000, AfterTokens: 8000, Summary: "second summary"})
	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "final"}}})

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, history, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(history) != 2 {
		t.Fatalf("len(history) = %d, want 2", len(history))
	}
	if history[0].Content[0].Text != "[Auto-compact summary]\n\nsecond summary" {
		t.Errorf("history[0] text = %q", history[0].Content[0].Text)
	}
	if history[1].Content[0].Text != "final" {
		t.Errorf("history[1] text = %q", history[1].Content[0].Text)
	}
}

func TestLoad_CompactionRetainsFullHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	w, err := OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}

	w.Emit(SessionStart{ID: "sess-h5", CWD: "/tmp", Model: "m"})

	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "old"}}})
	w.Emit(AssistantMessage{Content: []common.Block{{Kind: common.BlockText, Text: "old reply"}}})

	w.Emit(Compaction{
		BeforeTokens: 50000,
		AfterTokens:  10000,
		Summary:      "conversation summary",
		History: []common.Message{
			{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "first prompt"}}},
			{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "first reply"}}},
			{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: CompactSummaryPrefix + "conversation summary"}}},
			{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "recent q"}}},
			{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "recent a"}}},
		},
	})

	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "new"}}})
	w.Emit(AssistantMessage{Content: []common.Block{{Kind: common.BlockText, Text: "new reply"}}})

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, history, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// History should be: 5 from compaction snapshot + 2 post-compaction = 7
	if len(history) != 7 {
		t.Fatalf("len(history) = %d, want 7", len(history))
	}

	if history[0].Content[0].Text != "first prompt" {
		t.Errorf("history[0] = %q, want 'first prompt'", history[0].Content[0].Text)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, _, err := Load("/nonexistent/path/session.jsonl")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

// writeEvents writes the given events to a new JSONL file and returns its path.
func writeEvents(t *testing.T, events ...any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	w, err := OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	for _, ev := range events {
		w.Emit(ev)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return path
}

func TestLoad_LongLine(t *testing.T) {
	big := strings.Repeat("x", 70*1024)
	path := writeEvents(t,
		SessionStart{ID: "long", CWD: "/tmp", Model: "m", ModelConfig: "e", StartedAt: time.Now().UTC()},
		UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}},
		AssistantMessage{Content: []common.Block{
			{Kind: common.BlockToolUse, ToolUseID: "tu1", ToolName: "shell", InputJSON: `{}`},
		}},
		ToolResult{ToolUseID: "tu1", Content: big},
	)

	_, history, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("len(history) = %d, want 3", len(history))
	}
	if got := history[2].Content[0].ResultText; got != big {
		t.Errorf("result text length = %d, want %d", len(got), len(big))
	}
}

func TestLoad_GroupsParallelToolResults(t *testing.T) {
	path := writeEvents(t,
		SessionStart{ID: "grp", CWD: "/tmp", Model: "m", ModelConfig: "e", StartedAt: time.Now().UTC()},
		UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}},
		AssistantMessage{Content: []common.Block{
			{Kind: common.BlockToolUse, ToolUseID: "tu1", ToolName: "shell", InputJSON: `{}`},
			{Kind: common.BlockToolUse, ToolUseID: "tu2", ToolName: "shell", InputJSON: `{}`},
		}},
		ToolResult{ToolUseID: "tu1", Content: "one"},
		ToolResult{ToolUseID: "tu2", Content: "two"},
		UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "next"}}},
	)

	_, history, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(history) != 4 {
		t.Fatalf("len(history) = %d, want 4 (user, assistant, one tool, user)", len(history))
	}
	tool := history[2]
	if tool.Role != common.RoleTool || len(tool.Content) != 2 {
		t.Fatalf("history[2] = %+v, want one RoleTool message with 2 blocks", tool)
	}
	if tool.Content[0].ToolUseID != "tu1" || tool.Content[1].ToolUseID != "tu2" {
		t.Errorf("tool result order = %q, %q", tool.Content[0].ToolUseID, tool.Content[1].ToolUseID)
	}
}

func TestLoad_RepairsMissingToolResults(t *testing.T) {
	path := writeEvents(t,
		SessionStart{ID: "rep", CWD: "/tmp", Model: "m", ModelConfig: "e", StartedAt: time.Now().UTC()},
		UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}},
		AssistantMessage{Content: []common.Block{
			{Kind: common.BlockToolUse, ToolUseID: "tu1", ToolName: "shell", InputJSON: `{}`},
			{Kind: common.BlockToolUse, ToolUseID: "tu2", ToolName: "shell", InputJSON: `{}`},
			{Kind: common.BlockToolUse, ToolUseID: "tu3", ToolName: "shell", InputJSON: `{}`},
		}},
		ToolResult{ToolUseID: "tu2", Content: "two"},
		UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "steer"}}},
		AssistantMessage{Content: []common.Block{
			{Kind: common.BlockToolUse, ToolUseID: "tu4", ToolName: "shell", InputJSON: `{}`},
		}},
	)

	_, history, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// user, assistant, tool(tu1,tu2,tu3), user, assistant, tool(tu4)
	if len(history) != 6 {
		t.Fatalf("len(history) = %d, want 6: %+v", len(history), history)
	}
	tool := history[2]
	if tool.Role != common.RoleTool || len(tool.Content) != 3 {
		t.Fatalf("history[2] = %+v, want RoleTool with 3 blocks", tool)
	}
	// Results must follow the tool_use order, synthetic ones included.
	for i, want := range []string{"tu1", "tu2", "tu3"} {
		if got := tool.Content[i].ToolUseID; got != want {
			t.Errorf("tool.Content[%d].ToolUseID = %q, want %q", i, got, want)
		}
	}
	byID := map[string]common.Block{}
	for _, b := range tool.Content {
		byID[b.ToolUseID] = b
	}
	if b := byID["tu2"]; b.ResultText != "two" || b.IsError {
		t.Errorf("recorded result altered: %+v", b)
	}
	for _, id := range []string{"tu1", "tu3"} {
		b, ok := byID[id]
		if !ok || b.ResultText != "skipped: no result recorded" || !b.IsError {
			t.Errorf("%s not a synthetic skipped result: %+v", id, b)
		}
	}
	last := history[5]
	if last.Role != common.RoleTool || len(last.Content) != 1 || last.Content[0].ToolUseID != "tu4" {
		t.Errorf("trailing tool_use not repaired: %+v", last)
	}
}

// TestLoad_NonHistoryEventsBetweenToolUseAndResults shows why repair only
// needs to look at the message right after each assistant turn: the agent
// writes an assistant turn, then only non-history events (approval, subagent)
// before its results, and any steer user_message after them. Non-history
// events do not create messages, so the results stay next to their tool_use.
func TestLoad_NonHistoryEventsBetweenToolUseAndResults(t *testing.T) {
	path := writeEvents(t,
		SessionStart{ID: "adj", CWD: "/tmp", Model: "m", ModelConfig: "e", StartedAt: time.Now().UTC()},
		UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}},
		AssistantMessage{Content: []common.Block{
			{Kind: common.BlockToolUse, ToolUseID: "tu1", ToolName: "shell", InputJSON: `{}`},
			{Kind: common.BlockToolUse, ToolUseID: "tu2", ToolName: "subagent", InputJSON: `{}`},
		}},
		Approval{ToolUseID: "tu1", Outcome: "allow"},
		ToolResult{ToolUseID: "tu1", Content: "one"},
		SubagentSpawn{ChildID: "c1", Task: "t"},
		SubagentDone{ChildID: "c1"},
		ToolResult{ToolUseID: "tu2", Content: "two"},
		UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "steer"}}},
	)

	_, history, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// user, assistant, tool(tu1,tu2), user
	if len(history) != 4 {
		t.Fatalf("len(history) = %d, want 4: %+v", len(history), history)
	}
	tool := history[2]
	if tool.Role != common.RoleTool || len(tool.Content) != 2 {
		t.Fatalf("history[2] = %+v, want RoleTool with 2 blocks", tool)
	}
	for i, want := range []string{"one", "two"} {
		if b := tool.Content[i]; b.ResultText != want || b.IsError {
			t.Errorf("tool.Content[%d] = %+v, want recorded result %q", i, b, want)
		}
	}
}

// TestLoad_ContinuesSubagentNumbering checks that a resumed parent does not
// reuse child IDs already recorded by subagent_spawn markers.
func TestLoad_ContinuesSubagentNumbering(t *testing.T) {
	path := writeEvents(t,
		SessionStart{ID: "p1", CWD: "/tmp", Model: "m", ModelConfig: "e"},
		UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}},
		SubagentSpawn{ChildID: "p1-sub-1", Task: "a"},
		SubagentDone{ChildID: "p1-sub-1"},
		SubagentSpawn{ChildID: "p1-sub-2", Task: "b"},
		SubagentDone{ChildID: "p1-sub-2"},
	)
	sess, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := sess.NextSubagentID(); got != "p1-sub-3" {
		t.Errorf("NextSubagentID = %q, want %q", got, "p1-sub-3")
	}
}

func TestLoad_ThinkingAndRedactedRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	w, err := OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	w.Emit(SessionStart{ID: "s1", CWD: "/tmp", Model: "m", StartedAt: time.Now()})
	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}})
	want := []common.Block{
		{Kind: common.BlockThinking, Text: "a", Signature: "sig-1"},
		{Kind: common.BlockRedactedThinking, Data: "opaque=="},
		{Kind: common.BlockThinking, Signature: "sig-2"},
		{Kind: common.BlockText, Text: "hello"},
	}
	w.Emit(AssistantMessage{Content: want, StopReason: "end_turn"})
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, history, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history len = %d, want 2", len(history))
	}
	got := history[1].Content
	if len(got) != len(want) {
		t.Fatalf("blocks = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("block %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
