package anthropic

import (
	"os"
	"testing"
)

func TestParseSSE_Fixture(t *testing.T) {
	f, err := os.Open("testdata/simple_text.sse")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	frames := parseSSE(f)

	expected := []sseFrame{
		{event: "message_start", data: `{"type":"message_start","message":{"id":"msg_01XFDUDYJgAACzvnptvVoYEL","role":"assistant","model":"claude-3-5-haiku-20241022","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":1,"cache_read_input_tokens":2,"cache_creation_input_tokens":3}}}`},
		{event: "content_block_start", data: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{event: "content_block_delta", data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`},
		{event: "content_block_delta", data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":", world"}}`},
		{event: "content_block_delta", data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"!"}}`},
		{event: "content_block_stop", data: `{"type":"content_block_stop","index":0}`},
		{event: "message_delta", data: `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":15}}`},
		{event: "message_stop", data: `{"type":"message_stop"}`},
	}

	i := 0
	for frame := range frames {
		if i >= len(expected) {
			t.Errorf("got extra frame: event=%q data=%q", frame.event, frame.data)
			i++
			continue
		}
		if frame.event != expected[i].event {
			t.Errorf("frame %d event: got %q, want %q", i, frame.event, expected[i].event)
		}
		if frame.data != expected[i].data {
			t.Errorf("frame %d data: got %q, want %q", i, frame.data, expected[i].data)
		}
		i++
	}
	if i != len(expected) {
		t.Errorf("expected %d frames, got %d", len(expected), i)
	}
}
