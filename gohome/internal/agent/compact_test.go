package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
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

	// 8 messages: 2 prefix + 2 to summarize + 4 recent
	sess.History = []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "first"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "second"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply2"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "third"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply3"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "fourth"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply4"}}},
	}

	if err := a.compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}

	// Should be: 2 prefix + summary + 4 recent = 7 messages.
	if len(sess.History) != 7 {
		t.Fatalf("len(sess.History) = %d, want 7 (prefix + summary + recent)", len(sess.History))
	}

	// First two messages should be the stable prefix (unchanged).
	if sess.History[0].Content[0].Text != "first" {
		t.Errorf("prefix[0] = %q, want 'first'", sess.History[0].Content[0].Text)
	}
	if sess.History[1].Content[0].Text != "reply" {
		t.Errorf("prefix[1] = %q, want 'reply'", sess.History[1].Content[0].Text)
	}

	// Third message should be the summary.
	want := "[Auto-compact summary]\n\n" + summaryText
	if sess.History[2].Content[0].Text != want {
		t.Errorf("summary message = %q, want %q", sess.History[2].Content[0].Text, want)
	}

	// Last message should be unchanged from original.
	last := sess.History[len(sess.History)-1]
	if last.Content[0].Text != "reply4" {
		t.Errorf("last message = %q, want 'reply4'", last.Content[0].Text)
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
	// With prefix preservation (first 2 messages kept as stable prefix):
	//   [0] User              \  stable prefix
	//   [1] Assistant         /
	//   [2] User              -> summarized (oldMessages = history[2:3])
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

	// After compaction: 2 prefix + summary + messages[3:8] = 8 messages total
	// (splitIdx backed up from 4 to 3, so the tool pair stays intact).
	if len(sess.History) != 8 {
		t.Fatalf("len(sess.History) = %d, want 8 (2 prefix + summary + 5 kept)", len(sess.History))
	}

	// First two messages should be stable prefix.
	if sess.History[0].Content[0].Text != "old" {
		t.Errorf("prefix[0] = %q, want 'old'", sess.History[0].Content[0].Text)
	}
	if sess.History[1].Content[0].Text != "old reply" {
		t.Errorf("prefix[1] = %q, want 'old reply'", sess.History[1].Content[0].Text)
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

	// 8 messages: 2 prefix + 2 to summarize + 4 recent
	sess.History = []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "first"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "second"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply2"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "third"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply3"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "fourth"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply4"}}},
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

	// 8 messages: 2 prefix + 2 to summarize + 4 recent
	sess.History = []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "first"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "second"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply2"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "third"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply3"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "fourth"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply4"}}},
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

func TestCompact_PreservesStablePrefix(t *testing.T) {
	summaryText := "mid-conversation summary"
	events := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: summaryText},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	// 8 messages: 2 prefix + 2 to summarize + 4 recent
	sess.History = []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "initial prompt"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "initial response"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "middle question"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "middle answer"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "recent q1"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "recent a1"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "recent q2"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "recent a2"}}},
	}

	if err := a.compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}

	// Expected: 2 prefix + 1 summary + 4 recent = 7
	if len(sess.History) != 7 {
		t.Fatalf("len(sess.History) = %d, want 7", len(sess.History))
	}

	// Verify stable prefix is preserved exactly.
	if sess.History[0].Content[0].Text != "initial prompt" {
		t.Errorf("prefix[0] = %q, want 'initial prompt'", sess.History[0].Content[0].Text)
	}
	if sess.History[0].Role != common.RoleUser {
		t.Errorf("prefix[0].Role = %v, want RoleUser", sess.History[0].Role)
	}
	if sess.History[1].Content[0].Text != "initial response" {
		t.Errorf("prefix[1] = %q, want 'initial response'", sess.History[1].Content[0].Text)
	}
	if sess.History[1].Role != common.RoleAssistant {
		t.Errorf("prefix[1].Role = %v, want RoleAssistant", sess.History[1].Role)
	}

	// Verify summary message at index 2.
	if !strings.Contains(sess.History[2].Content[0].Text, session.CompactSummaryPrefix) {
		t.Errorf("summary message missing CompactSummaryPrefix, got %q", sess.History[2].Content[0].Text)
	}
	if !strings.Contains(sess.History[2].Content[0].Text, summaryText) {
		t.Errorf("summary message missing summary text, got %q", sess.History[2].Content[0].Text)
	}

	// Verify last 4 messages are the recent ones.
	recentTexts := []string{"recent q1", "recent a1", "recent q2", "recent a2"}
	for i, want := range recentTexts {
		got := sess.History[3+i].Content[0].Text
		if got != want {
			t.Errorf("recent[%d] = %q, want %q", i, got, want)
		}
	}
}

// capturingClient wraps fakeClient to capture the request sent to Stream.
type capturingClient struct {
	*fakeClient
	onStream func(common.Request)
}

func (c *capturingClient) Stream(ctx context.Context, req common.Request) (<-chan common.StreamEvent, error) {
	if c.onStream != nil {
		c.onStream(req)
	}
	return c.fakeClient.Stream(ctx, req)
}

func TestCompact_StripsToolBlocks(t *testing.T) {
	summaryText := "summary with tools"
	events := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: summaryText},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}

	var capturedReq common.Request
	client := &capturingClient{
		fakeClient: &fakeClient{sequences: [][]common.StreamEvent{events}},
		onStream:   func(req common.Request) { capturedReq = req },
	}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)
	a.State = NewSessionState(sess, a.State.Writer(), client)

	sess.History = []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "first"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "reply"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "run ls"}}},
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockText, Text: "calling tool"},
			{Kind: common.BlockToolUse, ToolUseID: "tc1", ToolName: "shell", InputJSON: `{"command":"ls"}`},
		}},
		{Role: common.RoleTool, Content: []common.Block{
			{Kind: common.BlockToolResult, ToolUseID: "tc1", ResultText: "file1.go"},
		}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "done"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "recent"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "recent reply"}}},
	}

	if err := a.compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}

	for i, msg := range capturedReq.Messages {
		for j, block := range msg.Content {
			if block.Kind == common.BlockToolUse {
				t.Errorf("request message[%d].block[%d] is tool_use, should be stripped", i, j)
			}
			if block.Kind == common.BlockToolResult {
				t.Errorf("request message[%d].block[%d] is tool_result, should be stripped", i, j)
			}
		}
		if msg.Role == common.RoleTool {
			t.Errorf("request message[%d] has RoleTool, should be converted", i)
		}
	}
}

func TestCompact_PrefixEndsWithToolUse(t *testing.T) {
	summaryText := "summary"
	events := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: summaryText},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	sess.History = []common.Message{
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "do something"}}},
		{Role: common.RoleAssistant, Content: []common.Block{
			{Kind: common.BlockToolUse, ToolUseID: "tc1", ToolName: "shell", InputJSON: `{"command":"ls"}`},
		}},
		{Role: common.RoleTool, Content: []common.Block{
			{Kind: common.BlockToolResult, ToolUseID: "tc1", ResultText: "files"},
		}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "q2"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "a2"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "q3"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "a3"}}},
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "q4"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "a4"}}},
	}

	if err := a.compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}

	for i, msg := range sess.History {
		if msg.Role == common.RoleAssistant {
			for _, b := range msg.Content {
				if b.Kind == common.BlockToolUse {
					if i+1 >= len(sess.History) || sess.History[i+1].Role != common.RoleTool {
						t.Errorf("tool_use at history[%d] has no following tool_result", i)
					}
				}
			}
		}
	}
}

func TestCompact_TooFewForPrefix(t *testing.T) {
	// 6 messages is below minRequired (7), so compact should be a no-op.
	client := &fakeClient{}
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

	if client.callCount != 0 {
		t.Errorf("client called %d times, want 0 (too few messages for prefix-aware compact)", client.callCount)
	}
	if len(sess.History) != 6 {
		t.Errorf("history length changed: got %d, want 6", len(sess.History))
	}
}
