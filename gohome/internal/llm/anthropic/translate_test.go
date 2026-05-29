package anthropic

import (
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// makeFrames is a helper that sends frames into translateEvents via a channel.
func makeFrames(frames []sseFrame) <-chan sseFrame {
	ch := make(chan sseFrame, len(frames))
	for _, f := range frames {
		ch <- f
	}
	close(ch)
	return ch
}

func collectEvents(ch <-chan common.StreamEvent) []common.StreamEvent {
	var events []common.StreamEvent
	for e := range ch {
		events = append(events, e)
	}
	return events
}

func TestTranslateEvents_TextDeltas(t *testing.T) {
	frames := []sseFrame{
		{event: "content_block_start", data: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{event: "content_block_delta", data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`},
		{event: "content_block_delta", data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":", world"}}`},
		{event: "content_block_delta", data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"!"}}`},
		{event: "content_block_stop", data: `{"type":"content_block_stop","index":0}`},
		{event: "message_stop", data: `{"type":"message_stop"}`},
	}

	events := collectEvents(translateEvents(makeFrames(frames)))

	// expect 3 EventTextDelta then 1 EventTurnDone
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d: %v", len(events), events)
	}

	expectedDeltas := []string{"Hello", ", world", "!"}
	for i, delta := range expectedDeltas {
		if events[i].Kind != common.EventTextDelta {
			t.Errorf("event %d kind: got %q, want EventTextDelta", i, events[i].Kind)
		}
		if events[i].TextDelta != delta {
			t.Errorf("event %d TextDelta: got %q, want %q", i, events[i].TextDelta, delta)
		}
	}

	if events[3].Kind != common.EventTurnDone {
		t.Errorf("last event kind: got %q, want EventTurnDone", events[3].Kind)
	}
}

func TestTranslateEvents_ToolUseAccumulation(t *testing.T) {
	frames := []sseFrame{
		{event: "content_block_start", data: `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_01","name":"read_file","input":{}}}`},
		{event: "content_block_delta", data: `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\""}}`},
		{event: "content_block_delta", data: `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":":{\"sub\":\"foo\"}}"}}`},
		{event: "content_block_stop", data: `{"type":"content_block_stop","index":0}`},
		{event: "message_stop", data: `{"type":"message_stop"}`},
	}

	events := collectEvents(translateEvents(makeFrames(frames)))

	// expect zero EventTextDelta, one EventToolCallDone, one EventTurnDone
	var textDeltas, toolDones, turnDones int
	for _, e := range events {
		switch e.Kind {
		case common.EventTextDelta:
			textDeltas++
		case common.EventToolCallDone:
			toolDones++
			if e.ToolCallID != "toolu_01" {
				t.Errorf("ToolCallID: got %q, want %q", e.ToolCallID, "toolu_01")
			}
			if e.ToolName != "read_file" {
				t.Errorf("ToolName: got %q, want %q", e.ToolName, "read_file")
			}
			wantInput := `{"path":{"sub":"foo"}}`
			if e.InputJSON != wantInput {
				t.Errorf("InputJSON: got %q, want %q", e.InputJSON, wantInput)
			}
		case common.EventTurnDone:
			turnDones++
		}
	}

	if textDeltas != 0 {
		t.Errorf("expected 0 EventTextDelta, got %d", textDeltas)
	}
	if toolDones != 1 {
		t.Errorf("expected 1 EventToolCallDone, got %d", toolDones)
	}
	if turnDones != 1 {
		t.Errorf("expected 1 EventTurnDone, got %d", turnDones)
	}
}

func TestTranslateEvents_UsageOnTurnDone(t *testing.T) {
	frames := []sseFrame{
		{event: "message_start", data: `{"type":"message_start","message":{"id":"msg_01","role":"assistant","model":"claude-3-5-haiku-20241022","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":1,"cache_read_input_tokens":2,"cache_creation_input_tokens":3}}}`},
		{event: "message_delta", data: `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":15}}`},
		{event: "message_stop", data: `{"type":"message_stop"}`},
	}

	events := collectEvents(translateEvents(makeFrames(frames)))

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.Kind != common.EventTurnDone {
		t.Fatalf("event kind: got %q, want EventTurnDone", e.Kind)
	}
	if e.StopReason != "end_turn" {
		t.Errorf("StopReason: got %q, want %q", e.StopReason, "end_turn")
	}
	if e.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if e.Usage.InputTokens != 10 {
		t.Errorf("InputTokens: got %d, want 10", e.Usage.InputTokens)
	}
	if e.Usage.OutputTokens != 15 {
		t.Errorf("OutputTokens: got %d, want 15", e.Usage.OutputTokens)
	}
	if e.Usage.CacheReadTokens != 2 {
		t.Errorf("CacheReadTokens: got %d, want 2", e.Usage.CacheReadTokens)
	}
	if e.Usage.CacheWriteTokens != 3 {
		t.Errorf("CacheWriteTokens: got %d, want 3", e.Usage.CacheWriteTokens)
	}
}
