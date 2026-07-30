package agent

import (
	"context"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

func TestShouldCompact_Disabled(t *testing.T) {
	cfg := CompactConfig{Enabled: false, Mode: "percentage", TriggerPct: 0.80, ContextWindow: 100000}
	usage := common.Usage{InputTokens: 90000, OutputTokens: 5000}
	if cfg.shouldCompact(usage) {
		t.Error("shouldCompact returned true when disabled")
	}
}

func TestShouldCompact_Percentage_Below(t *testing.T) {
	cfg := CompactConfig{Enabled: true, Mode: "percentage", TriggerPct: 0.80, ContextWindow: 100000}
	usage := common.Usage{InputTokens: 70000, OutputTokens: 5000}
	if cfg.shouldCompact(usage) {
		t.Error("shouldCompact returned true below threshold")
	}
}

func TestShouldCompact_Percentage_Above(t *testing.T) {
	cfg := CompactConfig{Enabled: true, Mode: "percentage", TriggerPct: 0.80, ContextWindow: 100000}
	usage := common.Usage{InputTokens: 75000, OutputTokens: 10000}
	if !cfg.shouldCompact(usage) {
		t.Error("shouldCompact returned false above threshold")
	}
}

func TestShouldCompact_Percentage_Exact(t *testing.T) {
	cfg := CompactConfig{Enabled: true, Mode: "percentage", TriggerPct: 0.80, ContextWindow: 100000}
	usage := common.Usage{InputTokens: 75000, OutputTokens: 5000}
	if !cfg.shouldCompact(usage) {
		t.Error("shouldCompact returned false at exact threshold")
	}
}

func TestShouldCompact_Leftover_Above(t *testing.T) {
	cfg := CompactConfig{Enabled: true, Mode: "leftover", Leftover: 32000, ContextWindow: 128000}
	usage := common.Usage{InputTokens: 80000, OutputTokens: 10000}
	if cfg.shouldCompact(usage) {
		t.Error("shouldCompact returned true when enough tokens remain")
	}
}

func TestShouldCompact_Leftover_Below(t *testing.T) {
	cfg := CompactConfig{Enabled: true, Mode: "leftover", Leftover: 32000, ContextWindow: 128000}
	usage := common.Usage{InputTokens: 90000, OutputTokens: 10000}
	if !cfg.shouldCompact(usage) {
		t.Error("shouldCompact returned false when tokens below leftover")
	}
}

func TestShouldCompact_ZeroContextWindow(t *testing.T) {
	cfg := CompactConfig{Enabled: true, Mode: "percentage", TriggerPct: 0.80, ContextWindow: 0}
	usage := common.Usage{InputTokens: 90000, OutputTokens: 5000}
	if cfg.shouldCompact(usage) {
		t.Error("shouldCompact returned true with zero context window")
	}
}

func TestCompact_KeepsRecentMessages(t *testing.T) {
	summaryText := "This is the compacted summary."
	events := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: summaryText},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	sess.History = []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "first"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "second"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply2"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "third"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply3"}}},
	}

	if err := a.compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}

	// Should be: summary + last 4 messages (2 turns kept).
	if len(sess.History) < 3 {
		t.Fatalf("len(sess.History) = %d, want >= 3 (summary + recent)", len(sess.History))
	}

	// First message should be the summary.
	want := "[Auto-compact summary]\n\n" + summaryText
	if sess.History[0].Content[0].Text != want {
		t.Errorf("first message = %q, want summary", sess.History[0].Content[0].Text)
	}

	// Last message should be unchanged from original.
	last := sess.History[len(sess.History)-1]
	if last.Content[0].Text != "reply3" {
		t.Errorf("last message = %q, want 'reply3'", last.Content[0].Text)
	}
}

func TestCompact_DoesNotSplitToolPair(t *testing.T) {
	summaryText := "summary"
	events := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: summaryText},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	// 8 messages so splitIdx = 8-4 = 4, which lands on the RoleTool
	// message at index 4. This forces the splitIdx-- boundary adjustment
	// to fire, backing splitIdx up to 3 so the assistant+tool pair stays
	// together in the kept portion.
	//
	//   [0] User              \
	//   [1] Assistant           > summarized (oldMessages = history[:3])
	//   [2] User              /
	//   [3] Assistant (tool_use)  <-- splitIdx backs up here
	//   [4] Tool (result)         <-- original splitIdx lands here
	//   [5] Assistant          \
	//   [6] User                > kept (recentMessages = history[3:])
	//   [7] Assistant          /
	sess.History = []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "old"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "old reply"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "do something"}}},
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockText, Text: "calling tool"},
			{Kind: common.BlockToolUse, ToolUseID: "tc1", ToolName: "shell", InputJSON: `{"command":"ls"}`},
		}},
		{Role: common.RoleTool, Content: []common.Block{
			{Kind: common.BlockToolResult, ToolUseID: "tc1", ResultText: "file1"},
		}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "here are your files"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "thanks"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "you're welcome"}}},
	}

	if err := a.compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}

	// After compaction: summary + messages[3:8] = 6 messages total
	// (splitIdx backed up from 4 to 3, so the tool pair stays intact).
	if len(sess.History) != 6 {
		t.Fatalf("len(sess.History) = %d, want 6 (summary + 5 kept)", len(sess.History))
	}

	// The tool_result message and its preceding assistant message should both
	// be in the kept portion -- never split apart.
	for i, msg := range sess.History {
		if msg.Role == common.RoleTool && i == 0 {
			t.Error("RoleTool should not be the first message (would be split from its assistant)")
		}
		if msg.Role == common.RoleTool && i > 0 {
			prev := sess.History[i-1]
			if prev.Role != common.RoleAssistant {
				t.Errorf("message before RoleTool should be assistant, got %v", prev.Role)
			}
		}
	}
}

func TestCompact_TooShortNoop(t *testing.T) {
	client := &fakeClient{}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	sess.History = []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "hello"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}},
	}

	if err := a.compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}

	// Should not have called the LLM at all.
	if client.callCount != 0 {
		t.Errorf("client called %d times, want 0 (too short to compact)", client.callCount)
	}

	// History should be unchanged.
	if len(sess.History) != 2 {
		t.Errorf("history length changed: got %d, want 2", len(sess.History))
	}
}

func TestCompact_EmitsEvent(t *testing.T) {
	events := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: "summary"},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	sess.History = []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "first"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "second"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply2"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "third"}}},
	}

	if err := a.compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}

	var found bool
	for _, ev := range fe.events {
		if ev.Kind == EventCompacted {
			found = true
			if ev.CompactBefore <= 0 {
				t.Errorf("CompactBefore = %d, want > 0", ev.CompactBefore)
			}
		}
	}
	if !found {
		t.Error("no EventCompacted emitted")
	}
}

func TestCompact_ErrorFromStream(t *testing.T) {
	events := []common.StreamEvent{
		{Kind: common.EventError, Err: context.DeadlineExceeded},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	sess.History = []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "first"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "second"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply2"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "third"}}},
	}
	origLen := len(sess.History)

	err := a.compact(context.Background(), sess)
	if err == nil {
		t.Fatal("expected error from compact, got nil")
	}

	if len(sess.History) != origLen {
		t.Errorf("history length changed: got %d, want %d", len(sess.History), origLen)
	}
}

func TestCompact_EmptyHistoryNoop(t *testing.T) {
	fe := &fakeRecorder{}
	client := &fakeClient{}
	a, sess, _ := newTestAgent(t, client, fe)

	sess.History = nil
	if err := a.compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}

	if len(fe.events) != 0 {
		t.Errorf("expected no events, got %d", len(fe.events))
	}
	if client.callCount != 0 {
		t.Errorf("client called %d times, want 0", client.callCount)
	}
}
