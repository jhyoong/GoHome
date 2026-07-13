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

func TestCompact_ReplacesHistory(t *testing.T) {
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
	}

	if err := a.compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}

	if len(sess.History) != 1 {
		t.Fatalf("len(sess.History) = %d, want 1", len(sess.History))
	}
	if sess.History[0].Role != common.RoleUser {
		t.Errorf("role = %q, want user", sess.History[0].Role)
	}
	want := "[Auto-compact summary]\n\n" + summaryText
	if sess.History[0].Content[0].Text != want {
		t.Errorf("text = %q, want %q", sess.History[0].Content[0].Text, want)
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
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "hello"}}},
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
		{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "hello"}}},
		{Role: common.RoleAssistant, Content: []common.Block{{Kind: common.BlockText, Text: "world"}}},
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
