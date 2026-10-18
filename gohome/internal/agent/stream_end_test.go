package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// TestTurn_ChannelClosedWithoutTurnDone verifies that a stream channel that
// closes without EventTurnDone is an error, not a completed turn.
func TestTurn_ChannelClosedWithoutTurnDone(t *testing.T) {
	events := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: "partial"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	_, _, err := a.Turn(context.Background(), sess)
	if err == nil {
		t.Fatal("expected error from Turn, got nil")
	}
	if len(sess.History) != 0 {
		t.Errorf("sess.History length: got %d, want 0", len(sess.History))
	}
	for _, ev := range fe.events {
		if ev.Kind == EventTurnDone {
			t.Error("unexpected EventTurnDone for a stream that ended early")
		}
	}
	if n, _ := countErrorEvents(fe); n != 1 {
		t.Errorf("EventError count: got %d, want 1", n)
	}
}

// TestTurn_TokenLimitStopReasonEmitsNotice verifies that max_tokens (Anthropic)
// and length (OpenAI) stop reasons show a notice to the user.
func TestTurn_TokenLimitStopReasonEmitsNotice(t *testing.T) {
	for _, reason := range []string{"max_tokens", "length"} {
		t.Run(reason, func(t *testing.T) {
			events := []common.StreamEvent{
				{Kind: common.EventTextDelta, TextDelta: "cut"},
				{Kind: common.EventTurnDone, StopReason: reason},
			}
			client := &fakeClient{sequences: [][]common.StreamEvent{events}}
			fe := &fakeRecorder{}
			a, sess, _ := newTestAgent(t, client, fe)

			if _, _, err := a.Turn(context.Background(), sess); err != nil {
				t.Fatalf("Turn: %v", err)
			}
			var notices int
			for _, ev := range fe.events {
				if ev.Kind == EventNotice {
					notices++
					if ev.Notice == "" {
						t.Error("EventNotice has empty Notice text")
					}
				}
			}
			if notices != 1 {
				t.Errorf("EventNotice count: got %d, want 1", notices)
			}
		})
	}
}

// TestTurn_InvalidToolJSONStoredAsEmptyObject verifies that a tool call with
// invalid input JSON is stored as {} and marked invalid.
func TestTurn_InvalidToolJSONStoredAsEmptyObject(t *testing.T) {
	events := []common.StreamEvent{
		{Kind: common.EventToolCallDone, ToolCallID: "tc1", ToolName: "shell", InputJSON: `{"command": "rm -rf build`},
		{Kind: common.EventTurnDone, StopReason: "max_tokens"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	if _, _, err := a.Turn(context.Background(), sess); err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if len(sess.History) != 1 {
		t.Fatalf("sess.History length: got %d, want 1", len(sess.History))
	}
	b := sess.History[0].Content[0]
	if b.InputJSON != "{}" {
		t.Errorf("InputJSON: got %q, want {}", b.InputJSON)
	}
	if !b.InvalidInput {
		t.Error("InvalidInput: got false, want true")
	}
}

// TestRun_InvalidToolJSONNotExecuted reproduces a truncated tool call. The
// tool must not run, an error result must be recorded, and the next request
// must hold only valid tool input JSON.
func TestRun_InvalidToolJSONNotExecuted(t *testing.T) {
	turn1 := []common.StreamEvent{
		{Kind: common.EventToolCallDone, ToolCallID: "tc1", ToolName: "fake", InputJSON: `{"command": "rm -rf build`},
		{Kind: common.EventTurnDone, StopReason: "max_tokens"},
	}
	turn2 := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: "retrying"},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	var requests []common.Request
	client := &capturingClient{
		fakeClient: &fakeClient{sequences: [][]common.StreamEvent{turn1, turn2}},
		onStream:   func(r common.Request) { requests = append(requests, r) },
	}

	count := 0
	reg := tools.NewRegistry()
	reg.Register(&countingTool{fakeTool: &fakeTool{name: "fake", content: "ran"}, count: &count})

	// Deny every approval: the invalid call must never reach the guard.
	fe := &fakeRecorder{}
	g := compileDenyGuard(t, fe)
	a, sess := newTestAgentWithGuard(t, client, fe, g, reg)

	if err := a.Run(context.Background(), sess); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if count != 0 {
		t.Errorf("tool executed %d times, want 0", count)
	}

	const wantMsg = "arguments were truncated or invalid JSON, retry"
	if len(sess.History) != 3 {
		t.Fatalf("sess.History length: got %d, want 3", len(sess.History))
	}
	res := sess.History[1]
	if res.Role != common.RoleTool || len(res.Content) != 1 {
		t.Fatalf("tool result message = %+v", res)
	}
	if !res.Content[0].IsError || res.Content[0].ResultText != wantMsg || res.Content[0].ToolUseID != "tc1" {
		t.Errorf("tool result block = %+v", res.Content[0])
	}

	var sawResult bool
	for _, ev := range fe.events {
		if ev.Kind == EventToolResult && ev.ToolCallID == "tc1" {
			sawResult = true
		}
	}
	if !sawResult {
		t.Error("no EventToolResult for tc1")
	}

	if len(requests) != 2 {
		t.Fatalf("requests: got %d, want 2", len(requests))
	}
	for _, m := range requests[1].Messages {
		for _, b := range m.Content {
			if b.Kind == common.BlockToolUse && !json.Valid([]byte(b.InputJSON)) {
				t.Errorf("next request holds invalid tool input %q", b.InputJSON)
			}
		}
	}

	if err := a.State.Writer().Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(sess.CWD(), "session.jsonl"))
	if err != nil {
		t.Fatalf("read jsonl: %v", err)
	}
	if got := countToolResultLines(t, string(raw), "tc1"); got != 1 {
		t.Errorf("tool_result lines for tc1 = %d, want 1\n%s", got, raw)
	}
	if strings.Contains(string(raw), `"approval"`) {
		t.Errorf("invalid call must not go through the guard\n%s", raw)
	}
}

// cancelMidStreamClient sends one text delta, then waits for ctx to be
// cancelled and closes the channel without EventTurnDone.
type cancelMidStreamClient struct {
	cancel context.CancelFunc
}

func (c *cancelMidStreamClient) Stream(ctx context.Context, _ common.Request) (<-chan common.StreamEvent, error) {
	ch := make(chan common.StreamEvent, 1)
	go func() {
		defer close(ch)
		ch <- common.StreamEvent{Kind: common.EventTextDelta, TextDelta: "partial"}
		c.cancel()
		<-ctx.Done()
	}()
	return ch, nil
}

// TestCompact_CancelMidStreamKeepsHistory verifies that cancelling during
// compaction leaves the history unchanged and writes no Compaction event.
func TestCompact_CancelMidStreamKeepsHistory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &cancelMidStreamClient{cancel: cancel}
	fe := &fakeRecorder{}
	a, sess, w := newTestAgent(t, client, fe)

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
	orig := make([]common.Message, len(sess.History))
	copy(orig, sess.History)

	if err := a.compact(ctx, sess); err == nil {
		t.Fatal("expected error from cancelled compact, got nil")
	}

	if len(sess.History) != len(orig) {
		t.Fatalf("history length: got %d, want %d", len(sess.History), len(orig))
	}
	for i := range orig {
		if sess.History[i].Content[0].Text != orig[i].Content[0].Text {
			t.Errorf("History[%d] = %q, want %q", i, sess.History[i].Content[0].Text, orig[i].Content[0].Text)
		}
	}
	for _, ev := range fe.events {
		if ev.Kind == EventCompacted {
			t.Error("unexpected EventCompacted")
		}
	}

	path := filepath.Join(sess.CWD(), "session.jsonl")
	for _, m := range readJSONLEvents(t, w, path) {
		var typ string
		_ = json.Unmarshal(m["type"], &typ)
		if typ == "compaction" {
			t.Error("Compaction event was written")
		}
	}
}

// TestCompact_StreamEndsEarlyKeepsHistory verifies that a compaction stream
// that closes without EventTurnDone, while ctx is still live, is an error,
// leaves the history unchanged and writes no Compaction event.
func TestCompact_StreamEndsEarlyKeepsHistory(t *testing.T) {
	events := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: "partial summary"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, w := newTestAgent(t, client, fe)

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
	orig := make([]common.Message, len(sess.History))
	copy(orig, sess.History)

	if err := a.compact(context.Background(), sess); !errors.Is(err, common.ErrStreamEndedEarly) {
		t.Fatalf("compact error = %v, want common.ErrStreamEndedEarly", err)
	}

	if len(sess.History) != len(orig) {
		t.Fatalf("history length: got %d, want %d", len(sess.History), len(orig))
	}
	for i := range orig {
		if sess.History[i].Content[0].Text != orig[i].Content[0].Text {
			t.Errorf("History[%d] = %q, want %q", i, sess.History[i].Content[0].Text, orig[i].Content[0].Text)
		}
	}
	for _, ev := range fe.events {
		if ev.Kind == EventCompacted {
			t.Error("unexpected EventCompacted")
		}
	}

	path := filepath.Join(sess.CWD(), "session.jsonl")
	for _, m := range readJSONLEvents(t, w, path) {
		var typ string
		_ = json.Unmarshal(m["type"], &typ)
		if typ == "compaction" {
			t.Error("Compaction event was written")
		}
	}
}
