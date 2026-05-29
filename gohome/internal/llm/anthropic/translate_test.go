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
