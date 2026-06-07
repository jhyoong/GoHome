package anthropic

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// --- JSON shapes for SSE data payloads ---

type msgStartData struct {
	Type    string `json:"type"`
	Message struct {
		Usage msgStartUsage `json:"usage"`
	} `json:"message"`
}

type msgStartUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

type msgDeltaData struct {
	Type  string `json:"type"`
	Delta struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type contentBlockStartData struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock struct {
		Type string `json:"type"`
		// for tool_use
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
}

type contentBlockDeltaData struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`         // for text_delta
		PartialJSON string `json:"partial_json"` // for input_json_delta
		Thinking    string `json:"thinking"`     // for thinking_delta
	} `json:"delta"`
}

// toolBlock accumulates state for an open tool_use content block.
type toolBlock struct {
	id       string
	name     string
	inputBuf []byte
}

// translateEvents converts a channel of SSE frames to common.StreamEvent.
// It maintains a state machine tracking open content blocks by index.
// When ctx is cancelled, the goroutine exits promptly.
func translateEvents(ctx context.Context, frames <-chan sseFrame) <-chan common.StreamEvent {
	ch := make(chan common.StreamEvent, 16)

	go func() {
		defer close(ch)

		// blockTypes tracks whether index N is "text" or "tool_use"
		blockTypes := map[int]string{}
		// toolBlocks accumulates tool_use state per block index
		toolBlocks := map[int]*toolBlock{}

		var usage common.Usage
		var stopReason string

		send := func(e common.StreamEvent) bool {
			select {
			case ch <- e:
				return true
			case <-ctx.Done():
				return false
			}
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
				return
			}

			switch frame.event {
			case "error":
				send(common.StreamEvent{Kind: common.EventError, Err: fmt.Errorf("%s", frame.data)})
				return

			case "message_start":
				var d msgStartData
				if err := json.Unmarshal([]byte(frame.data), &d); err != nil {
					send(common.StreamEvent{Kind: common.EventError, Err: err})
					return
				}
				usage.InputTokens = d.Message.Usage.InputTokens
				usage.CacheReadTokens = d.Message.Usage.CacheReadInputTokens
				usage.CacheWriteTokens = d.Message.Usage.CacheCreationInputTokens

			case "content_block_start":
				var d contentBlockStartData
				if err := json.Unmarshal([]byte(frame.data), &d); err != nil {
					send(common.StreamEvent{Kind: common.EventError, Err: err})
					return
				}
				blockTypes[d.Index] = d.ContentBlock.Type
				if d.ContentBlock.Type == "tool_use" {
					toolBlocks[d.Index] = &toolBlock{
						id:   d.ContentBlock.ID,
						name: d.ContentBlock.Name,
					}
				}

			case "content_block_delta":
				var d contentBlockDeltaData
				if err := json.Unmarshal([]byte(frame.data), &d); err != nil {
					send(common.StreamEvent{Kind: common.EventError, Err: err})
					return
				}
				switch d.Delta.Type {
				case "text_delta":
					if !send(common.StreamEvent{
						Kind:      common.EventTextDelta,
						TextDelta: d.Delta.Text,
					}) {
						return
					}
				case "thinking_delta":
					if !send(common.StreamEvent{
						Kind:          common.EventThinkingDelta,
						ThinkingDelta: d.Delta.Thinking,
					}) {
						return
					}
				case "input_json_delta":
					if tb, ok := toolBlocks[d.Index]; ok {
						tb.inputBuf = append(tb.inputBuf, []byte(d.Delta.PartialJSON)...)
					}
				}

			case "content_block_stop":
				// parse the index to finalize tool blocks
				var raw struct {
					Index int `json:"index"`
				}
				if err := json.Unmarshal([]byte(frame.data), &raw); err != nil {
					send(common.StreamEvent{Kind: common.EventError, Err: err})
					return
				}
				switch blockTypes[raw.Index] {
				case "thinking":
					if !send(common.StreamEvent{
						Kind: common.EventThinkingDone,
					}) {
						return
					}
				case "tool_use":
					tb := toolBlocks[raw.Index]
					if tb != nil {
						if !send(common.StreamEvent{
							Kind:       common.EventToolCallDone,
							ToolCallID: tb.id,
							ToolName:   tb.name,
							InputJSON:  string(tb.inputBuf),
						}) {
							return
						}
					}
					delete(toolBlocks, raw.Index)
				}
				delete(blockTypes, raw.Index)

			case "message_delta":
				var d msgDeltaData
				if err := json.Unmarshal([]byte(frame.data), &d); err != nil {
					send(common.StreamEvent{Kind: common.EventError, Err: err})
					return
				}
				stopReason = d.Delta.StopReason
				usage.OutputTokens = d.Usage.OutputTokens

			case "message_stop":
				u := usage // copy
				send(common.StreamEvent{
					Kind:       common.EventTurnDone,
					StopReason: stopReason,
					Usage:      &u,
				})

			// Unknown event types (e.g. "ping") are silently ignored.
			default:
			}
		}
	}()

	return ch
}
