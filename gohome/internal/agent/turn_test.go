package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// fakeClient is a fake common.Client that replays a scripted sequence of events.
type fakeClient struct {
	// sequences is a list of event slices; each call to Stream pops from the front.
	sequences [][]common.StreamEvent
	callCount int
}

func (c *fakeClient) Stream(_ context.Context, _ common.Request) (<-chan common.StreamEvent, error) {
	if c.callCount >= len(c.sequences) {
		// Return an empty channel that is already closed.
		ch := make(chan common.StreamEvent)
		close(ch)
		return ch, nil
	}
	events := c.sequences[c.callCount]
	c.callCount++
	ch := make(chan common.StreamEvent, len(events))
	for _, ev := range events {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

// newTestAgent creates a minimal Agent wired to a temp session file.
// NOTE: Guard is left nil. This is only safe for Turn-only tests where
// no tool dispatching occurs (Run/dispatchTool would panic on a nil Guard).
func newTestAgent(t *testing.T, client common.Client, fe Frontend) (*Agent, *session.Session, *session.Writer) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	w, err := session.OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	sess := session.NewSession("sess-test", dir, "claude-3-5-haiku", "anthropic")
	a := &Agent{
		Client:   client,
		Tools:    tools.NewRegistry(),
		Frontend: fe,
		Writer:   w,
		System:   "you are a test assistant",
	}
	return a, sess, w
}

// readJSONLEvents reads and decodes every JSONL line from path.
func readJSONLEvents(t *testing.T, w *session.Writer, path string) []map[string]json.RawMessage {
	t.Helper()
	// Close the writer first to flush.
	if err := w.Close(); err != nil {
		t.Fatalf("writer Close: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open session file: %v", err)
	}
	defer func() { _ = f.Close() }()

	var out []map[string]json.RawMessage
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("unmarshal JSONL line: %v\nraw: %s", err, line)
		}
		out = append(out, m)
	}
	return out
}

// TestTurn_TextDeltaAndTurnDone verifies:
//   - text deltas are forwarded to the frontend as EventTokenDelta events
//   - sess.History gains one assistant message with the concatenated text
//   - EventUsageUpdated and EventTurnDone are emitted after the stream
//   - an AssistantMessage event is persisted to the writer
func TestTurn_TextDeltaAndTurnDone(t *testing.T) {
	usage := &common.Usage{InputTokens: 10, OutputTokens: 5}
	events := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: "Hello"},
		{Kind: common.EventTextDelta, TextDelta: ", world"},
		{Kind: common.EventTurnDone, StopReason: "end_turn", Usage: usage},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}

	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	w, err := session.OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}

	sess := session.NewSession("sess-test", dir, "claude-3-5-haiku", "anthropic")
	a := &Agent{
		Client:   client,
		Tools:    tools.NewRegistry(),
		Frontend: fe,
		Writer:   w,
		System:   "system prompt",
	}

	stopReason, err := a.Turn(context.Background(), sess)
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if stopReason != "end_turn" {
		t.Errorf("stopReason: got %q, want %q", stopReason, "end_turn")
	}

	// History should have one assistant message.
	if len(sess.History) != 1 {
		t.Fatalf("sess.History length: got %d, want 1", len(sess.History))
	}
	msg := sess.History[0]
	if msg.Role != common.RoleAssistant {
		t.Errorf("role: got %v, want RoleAssistant", msg.Role)
	}
	if len(msg.Content) != 1 {
		t.Fatalf("content blocks: got %d, want 1", len(msg.Content))
	}
	if msg.Content[0].Kind != common.BlockText {
		t.Errorf("block kind: got %v, want BlockText", msg.Content[0].Kind)
	}
	if msg.Content[0].Text != "Hello, world" {
		t.Errorf("text: got %q, want %q", msg.Content[0].Text, "Hello, world")
	}

	// Check Frontend events: two token_delta + one usage_updated + one turn_done.
	wantKinds := []EventKind{EventTokenDelta, EventTokenDelta, EventUsageUpdated, EventTurnDone}
	if len(fe.events) != len(wantKinds) {
		t.Fatalf("frontend events: got %d, want %d\nevents: %+v", len(fe.events), len(wantKinds), fe.events)
	}
	for i, wk := range wantKinds {
		if fe.events[i].Kind != wk {
			t.Errorf("fe.events[%d].Kind: got %v, want %v", i, fe.events[i].Kind, wk)
		}
	}
	if fe.events[0].TextDelta != "Hello" {
		t.Errorf("first delta: got %q, want %q", fe.events[0].TextDelta, "Hello")
	}
	if fe.events[1].TextDelta != ", world" {
		t.Errorf("second delta: got %q, want %q", fe.events[1].TextDelta, ", world")
	}
	if fe.events[2].Usage != usage {
		t.Errorf("usage event: usage pointer mismatch")
	}
	if fe.events[3].StopReason != "end_turn" {
		t.Errorf("turn_done StopReason: got %q, want %q", fe.events[3].StopReason, "end_turn")
	}

	// Verify writer persisted an AssistantMessage.
	lines := readJSONLEvents(t, w, path)
	var foundAssistant bool
	for _, m := range lines {
		var typStr string
		if err := json.Unmarshal(m["type"], &typStr); err != nil {
			continue
		}
		if typStr == "assistant_message" {
			foundAssistant = true
			var stopRaw string
			if err := json.Unmarshal(m["stopReason"], &stopRaw); err != nil {
				t.Errorf("stopReason unmarshal: %v", err)
			} else if stopRaw != "end_turn" {
				t.Errorf("assistant_message stopReason: got %q, want %q", stopRaw, "end_turn")
			}
		}
	}
	if !foundAssistant {
		t.Errorf("no assistant_message event found in JSONL")
	}
}

// TestTurn_ToolUseBlock verifies that a tool_use block is accumulated and
// forwarded as EventToolCallDone.
func TestTurn_ToolUseBlock(t *testing.T) {
	events := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: "thinking..."},
		{Kind: common.EventToolCallDone, ToolCallID: "tc1", ToolName: "read", InputJSON: `{"path":"/tmp/x"}`},
		{Kind: common.EventTurnDone, StopReason: "tool_use"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	stopReason, err := a.Turn(context.Background(), sess)
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if stopReason != "tool_use" {
		t.Errorf("stopReason: got %q, want %q", stopReason, "tool_use")
	}

	// History: one assistant message.
	if len(sess.History) != 1 {
		t.Fatalf("sess.History length: got %d, want 1", len(sess.History))
	}
	blocks := sess.History[0].Content
	// Expect: text block first, then tool_use block.
	if len(blocks) != 2 {
		t.Fatalf("blocks count: got %d, want 2", len(blocks))
	}
	if blocks[0].Kind != common.BlockText {
		t.Errorf("blocks[0].Kind: got %v, want BlockText", blocks[0].Kind)
	}
	if blocks[1].Kind != common.BlockToolUse {
		t.Errorf("blocks[1].Kind: got %v, want BlockToolUse", blocks[1].Kind)
	}
	if blocks[1].ToolUseID != "tc1" {
		t.Errorf("ToolUseID: got %q, want tc1", blocks[1].ToolUseID)
	}
	if blocks[1].ToolName != "read" {
		t.Errorf("ToolName: got %q, want read", blocks[1].ToolName)
	}

	// Frontend should have seen a EventToolCallDone event.
	var sawToolDone bool
	for _, ev := range fe.events {
		if ev.Kind == EventToolCallDone {
			sawToolDone = true
			if ev.ToolCallID != "tc1" {
				t.Errorf("ToolCallID: got %q, want tc1", ev.ToolCallID)
			}
		}
	}
	if !sawToolDone {
		t.Errorf("no EventToolCallDone in frontend events")
	}
}

// TestTurn_StreamError verifies that a common.EventError surfaces as Turn returning err.
func TestTurn_StreamError(t *testing.T) {
	streamErr := context.DeadlineExceeded
	events := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: "partial"},
		{Kind: common.EventError, Err: streamErr},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	_, err := a.Turn(context.Background(), sess)
	if err == nil {
		t.Fatal("expected error from Turn, got nil")
	}

	// Frontend should have received an EventError.
	var sawErr bool
	for _, ev := range fe.events {
		if ev.Kind == EventError {
			sawErr = true
		}
	}
	if !sawErr {
		t.Errorf("frontend did not receive EventError")
	}
	_ = sess
}

// TestTurn_TextOnlyNoToolUse verifies no tool_use block produces a pure-text assistant message.
func TestTurn_TextOnlyNoToolUse(t *testing.T) {
	events := []common.StreamEvent{
		{Kind: common.EventTextDelta, TextDelta: "pure text"},
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{events}}
	fe := &fakeRecorder{}
	a, sess, _ := newTestAgent(t, client, fe)

	_, err := a.Turn(context.Background(), sess)
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if len(sess.History) != 1 {
		t.Fatalf("history length: got %d", len(sess.History))
	}
	if len(sess.History[0].Content) != 1 {
		t.Errorf("expected 1 block (text only), got %d", len(sess.History[0].Content))
	}
}
