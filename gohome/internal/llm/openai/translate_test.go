package openai

import (
	"context"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// makeFrames sends a slice of sseFrame into a buffered channel, then closes it.
func makeFrames(frames []sseFrame) <-chan sseFrame {
	ch := make(chan sseFrame, len(frames))
	for _, f := range frames {
		ch <- f
	}
	close(ch)
	return ch
}

// collectEvents drains a StreamEvent channel into a slice.
func collectEvents(ch <-chan common.StreamEvent) []common.StreamEvent {
	var events []common.StreamEvent
	for e := range ch {
		events = append(events, e)
	}
	return events
}

// --- Task 4.4: text deltas ---

func TestTranslateEvents_TextDeltas(t *testing.T) {
	frames := []sseFrame{
		{data: `{"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`},
		{data: `{"id":"c1","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`},
		{data: `{"id":"c1","choices":[{"index":0,"delta":{"content":", world"},"finish_reason":null}]}`},
		{data: `{"id":"c1","choices":[{"index":0,"delta":{"content":"!"},"finish_reason":null}]}`},
		{data: `{"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`},
		{data: `{"id":"c1","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":15,"total_tokens":25}}`},
		{done: true},
	}

	events := collectEvents(translateEvents(context.Background(), makeFrames(frames)))

	// expect 3 EventTextDelta + 1 EventTurnDone
	var textDeltas []string
	var turnDone *common.StreamEvent
	for _, e := range events {
		switch e.Kind {
		case common.EventTextDelta:
			textDeltas = append(textDeltas, e.TextDelta)
		case common.EventTurnDone:
			cp := e
			turnDone = &cp
		case common.EventError:
			t.Fatalf("unexpected error event: %v", e.Err)
		}
	}

	expectedDeltas := []string{"Hello", ", world", "!"}
	if len(textDeltas) != len(expectedDeltas) {
		t.Fatalf("expected %d text deltas, got %d: %v", len(expectedDeltas), len(textDeltas), textDeltas)
	}
	for i, want := range expectedDeltas {
		if textDeltas[i] != want {
			t.Errorf("delta %d: got %q, want %q", i, textDeltas[i], want)
		}
	}

	if turnDone == nil {
		t.Fatal("no EventTurnDone received")
	}
}

func TestTranslateEvents_EmptyContentDeltaSkipped(t *testing.T) {
	// The role-announcement delta with content="" must not emit an EventTextDelta.
	frames := []sseFrame{
		{data: `{"choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`},
		{done: true},
	}
	events := collectEvents(translateEvents(context.Background(), makeFrames(frames)))

	for _, e := range events {
		if e.Kind == common.EventTextDelta {
			t.Errorf("expected no EventTextDelta for empty content, got one: %q", e.TextDelta)
		}
	}
}

// --- Task 4.5: tool_calls accumulation ---

func TestTranslateEvents_ToolCallsAccumulation(t *testing.T) {
	frames := []sseFrame{
		// first delta: id and name arrive
		{data: `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_01","type":"function","function":{"name":"read_file","arguments":""}}]},"finish_reason":null}]}`},
		// subsequent deltas: arguments stream in
		{data: `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]},"finish_reason":null}]}`},
		{data: `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"/tmp\"}"}}]},"finish_reason":null}]}`},
		// finish_reason: tool_calls
		{data: `{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`},
		{data: `{"choices":[],"usage":{"prompt_tokens":20,"completion_tokens":30,"total_tokens":50}}`},
		{done: true},
	}

	events := collectEvents(translateEvents(context.Background(), makeFrames(frames)))

	var toolDones []common.StreamEvent
	var turnDone *common.StreamEvent
	for _, e := range events {
		switch e.Kind {
		case common.EventToolCallDone:
			toolDones = append(toolDones, e)
		case common.EventTurnDone:
			cp := e
			turnDone = &cp
		case common.EventError:
			t.Fatalf("unexpected error event: %v", e.Err)
		}
	}

	if len(toolDones) != 1 {
		t.Fatalf("expected 1 EventToolCallDone, got %d", len(toolDones))
	}
	td := toolDones[0]
	if td.ToolCallID != "call_01" {
		t.Errorf("ToolCallID: got %q, want call_01", td.ToolCallID)
	}
	if td.ToolName != "read_file" {
		t.Errorf("ToolName: got %q, want read_file", td.ToolName)
	}
	wantArgs := `{"path":"/tmp"}`
	if td.InputJSON != wantArgs {
		t.Errorf("InputJSON: got %q, want %q", td.InputJSON, wantArgs)
	}

	if turnDone == nil {
		t.Fatal("no EventTurnDone received")
	}
	if turnDone.StopReason != "tool_calls" {
		t.Errorf("StopReason: got %q, want tool_calls", turnDone.StopReason)
	}
}

func TestTranslateEvents_MultipleToolCalls(t *testing.T) {
	// Two tool calls with different indices.
	frames := []sseFrame{
		{data: `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_A","type":"function","function":{"name":"tool_a","arguments":"{\"x\":1}"}}]},"finish_reason":null}]}`},
		{data: `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_B","type":"function","function":{"name":"tool_b","arguments":"{\"y\":2}"}}]},"finish_reason":null}]}`},
		{data: `{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`},
		{done: true},
	}

	events := collectEvents(translateEvents(context.Background(), makeFrames(frames)))

	ids := map[string]string{}
	for _, e := range events {
		if e.Kind == common.EventToolCallDone {
			ids[e.ToolCallID] = e.ToolName
		}
	}

	if ids["call_A"] != "tool_a" {
		t.Errorf("call_A -> tool_a: got %q", ids["call_A"])
	}
	if ids["call_B"] != "tool_b" {
		t.Errorf("call_B -> tool_b: got %q", ids["call_B"])
	}
	if len(ids) != 2 {
		t.Errorf("expected 2 tool calls, got %d", len(ids))
	}
}

// --- Task 4.6: usage + turn done ---

func TestTranslateEvents_UsageOnTurnDone(t *testing.T) {
	frames := []sseFrame{
		{data: `{"choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":null}]}`},
		{data: `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`},
		{data: `{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":15,"total_tokens":25}}`},
		{done: true},
	}

	events := collectEvents(translateEvents(context.Background(), makeFrames(frames)))

	var turnDone *common.StreamEvent
	for _, e := range events {
		if e.Kind == common.EventTurnDone {
			cp := e
			turnDone = &cp
		}
	}

	if turnDone == nil {
		t.Fatal("no EventTurnDone")
	}
	if turnDone.StopReason != "stop" {
		t.Errorf("StopReason: got %q, want stop", turnDone.StopReason)
	}
	if turnDone.Usage == nil {
		t.Fatal("Usage is nil on TurnDone")
	}
	if turnDone.Usage.InputTokens != 10 {
		t.Errorf("InputTokens: got %d, want 10", turnDone.Usage.InputTokens)
	}
	if turnDone.Usage.OutputTokens != 15 {
		t.Errorf("OutputTokens: got %d, want 15", turnDone.Usage.OutputTokens)
	}
	// Cache tokens not provided by OpenAI; must be 0.
	if turnDone.Usage.CacheReadTokens != 0 || turnDone.Usage.CacheWriteTokens != 0 {
		t.Error("expected zero cache tokens for OpenAI")
	}
}

func TestTranslateEvents_TurnDoneWithoutUsageChunk(t *testing.T) {
	// If no usage chunk arrives before [DONE], TurnDone.Usage should be nil.
	frames := []sseFrame{
		{data: `{"choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":null}]}`},
		{data: `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`},
		{done: true},
	}

	events := collectEvents(translateEvents(context.Background(), makeFrames(frames)))

	var turnDone *common.StreamEvent
	for _, e := range events {
		if e.Kind == common.EventTurnDone {
			cp := e
			turnDone = &cp
		}
	}

	if turnDone == nil {
		t.Fatal("no EventTurnDone")
	}
	if turnDone.Usage != nil {
		t.Error("expected nil Usage when no usage chunk received")
	}
}

func TestTranslateEvents_CtxCancellationNoLeak(t *testing.T) {
	framesCh := make(chan sseFrame, 8)
	framesCh <- sseFrame{data: `{"choices":[{"index":0,"delta":{"content":"A"},"finish_reason":null}]}`}
	framesCh <- sseFrame{data: `{"choices":[{"index":0,"delta":{"content":"B"},"finish_reason":null}]}`}

	ctx, cancel := context.WithCancel(context.Background())
	out := translateEvents(ctx, framesCh)

	<-out
	<-out
	cancel()
	for range out {
	}
}
