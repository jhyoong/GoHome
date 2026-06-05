package openai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// --- JSON shapes for OpenAI stream chunk payloads ---

// chunkDelta is the delta object inside a choice.
type chunkDelta struct {
	Role      string          `json:"role"`
	Content   *string         `json:"content"`
	ToolCalls []chunkToolCall `json:"tool_calls"`
}

// chunkToolCall is a single tool_call delta element.
type chunkToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// chunkChoice is one element of the choices array.
type chunkChoice struct {
	Index        int        `json:"index"`
	Delta        chunkDelta `json:"delta"`
	FinishReason *string    `json:"finish_reason"`
}

// usagePayload is the usage object in the final usage chunk.
type usagePayload struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// chunk is the top-level OpenAI stream chunk.
type chunk struct {
	Choices []chunkChoice `json:"choices"`
	Usage   *usagePayload `json:"usage"`
}

// toolState accumulates state for an in-progress tool call keyed by index.
type toolState struct {
	id      string
	name    string
	argsBuf []byte
}

// translateEvents converts a channel of OpenAI sseFrames to common.StreamEvent values.
//
// Text delta handling (Task 4.4):
//
//	Non-empty delta.content emits EventTextDelta.
//
// Tool call accumulation (Task 4.5):
//
//	delta.tool_calls elements are keyed by index; arguments strings are
//	concatenated. When [DONE] is received (or finish_reason=="tool_calls"),
//	one EventToolCallDone is emitted per accumulated tool call.
//
// Usage + turn done (Task 4.6):
//
//	finish_reason from a choice sets the stop reason. A usage chunk
//	(choices empty, usage non-nil) captures token counts. On [DONE],
//	EventTurnDone is emitted with StopReason and non-nil Usage only when
//	a usage chunk was received.
func translateEvents(ctx context.Context, frames <-chan sseFrame) <-chan common.StreamEvent {
	ch := make(chan common.StreamEvent, 16)

	go func() {
		defer close(ch)

		// keyed by tool_call index
		tools := map[int]*toolState{}
		// tool call order (for deterministic emission)
		var toolOrder []int

		var stopReason string
		var usage *common.Usage

		send := func(e common.StreamEvent) bool {
			select {
			case ch <- e:
				return true
			case <-ctx.Done():
				return false
			}
		}

		// emitToolCalls drains accumulated tool calls in index order.
		emitToolCalls := func() bool {
			for _, idx := range toolOrder {
				ts := tools[idx]
				if ts == nil {
					continue
				}
				if !send(common.StreamEvent{
					Kind:       common.EventToolCallDone,
					ToolCallID: ts.id,
					ToolName:   ts.name,
					InputJSON:  string(ts.argsBuf),
				}) {
					return false
				}
			}
			return true
		}

		for {
			var frame sseFrame
			var ok bool
			select {
			case frame, ok = <-frames:
			case <-ctx.Done():
				return
			}
			if !ok {
				// frames channel closed without [DONE]; treat as end of stream.
				emitToolCalls()
				send(common.StreamEvent{
					Kind:       common.EventTurnDone,
					StopReason: stopReason,
					Usage:      usage,
				})
				return
			}

			// Error frame from the parser.
			if frame.err != nil {
				send(common.StreamEvent{Kind: common.EventError, Err: frame.err})
				return
			}

			// [DONE] sentinel: emit accumulated tool calls, then TurnDone.
			if frame.done {
				if !emitToolCalls() {
					return
				}
				send(common.StreamEvent{
					Kind:       common.EventTurnDone,
					StopReason: stopReason,
					Usage:      usage,
				})
				return
			}

			// Parse the JSON chunk.
			var c chunk
			if err := json.Unmarshal([]byte(frame.data), &c); err != nil {
				send(common.StreamEvent{
					Kind: common.EventError,
					Err:  fmt.Errorf("openai: parse chunk: %w", err),
				})
				return
			}

			// Usage chunk: choices is empty, usage is non-nil.
			if len(c.Choices) == 0 && c.Usage != nil {
				usage = &common.Usage{
					InputTokens:  c.Usage.PromptTokens,
					OutputTokens: c.Usage.CompletionTokens,
				}
				continue
			}

			// Process each choice.
			for _, choice := range c.Choices {
				// Capture finish_reason if present.
				if choice.FinishReason != nil && *choice.FinishReason != "" {
					stopReason = *choice.FinishReason
				}

				delta := choice.Delta

				// Text delta: emit only when content is non-nil and non-empty.
				if delta.Content != nil && *delta.Content != "" {
					if !send(common.StreamEvent{
						Kind:      common.EventTextDelta,
						TextDelta: *delta.Content,
					}) {
						return
					}
				}

				// Tool call deltas: accumulate by index.
				for _, tc := range delta.ToolCalls {
					ts, exists := tools[tc.Index]
					if !exists {
						ts = &toolState{}
						tools[tc.Index] = ts
						toolOrder = append(toolOrder, tc.Index)
					}
					if tc.ID != "" {
						ts.id = tc.ID
					}
					if tc.Function.Name != "" {
						ts.name = tc.Function.Name
					}
					ts.argsBuf = append(ts.argsBuf, []byte(tc.Function.Arguments)...)
				}
			}
		}
	}()

	return ch
}
