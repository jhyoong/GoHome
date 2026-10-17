package common

import (
	"context"
	"encoding/json"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type BlockKind string

const (
	BlockText       BlockKind = "text"
	BlockToolUse    BlockKind = "tool_use"
	BlockToolResult BlockKind = "tool_result"
	BlockThinking   BlockKind = "thinking"
	// BlockRedactedThinking holds encrypted thinking in Data. It is sent back
	// to the Anthropic API unchanged and never shown or sent to OpenAI.
	BlockRedactedThinking BlockKind = "redacted_thinking"
)

type Block struct {
	Kind       BlockKind `json:"kind"`
	Text       string    `json:"text,omitempty"`
	Signature  string    `json:"signature,omitempty"`
	Data       string    `json:"data,omitempty"` // redacted_thinking payload
	ToolUseID  string    `json:"toolUseId,omitempty"`
	ToolName   string    `json:"toolName,omitempty"`
	InputJSON  string    `json:"inputJson,omitempty"`
	ResultText string    `json:"resultText,omitempty"`
	IsError    bool      `json:"isError,omitempty"`
	// InvalidInput marks a tool_use whose streamed input was not valid JSON
	// (InputJSON then holds "{}"). In memory only; never persisted or sent.
	InvalidInput bool `json:"-"`
}

type Message struct {
	Role    Role    `json:"role"`
	Content []Block `json:"content"`
}

type EventKind string

const (
	EventTextDelta       EventKind = "text_delta"
	EventToolCallPartial EventKind = "tool_call_partial"
	EventToolCallDone    EventKind = "tool_call_done"
	EventTurnDone        EventKind = "turn_done"
	EventError           EventKind = "error"
	EventThinkingDelta   EventKind = "thinking_delta"
	EventThinkingDone    EventKind = "thinking_done"
	// EventRedactedThinking carries one whole redacted_thinking block in Data.
	EventRedactedThinking EventKind = "redacted_thinking"
)

type Usage struct {
	InputTokens      int `json:"inputTokens"`
	OutputTokens     int `json:"outputTokens"`
	CacheReadTokens  int `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int `json:"cacheWriteTokens,omitempty"`
}

type StreamEvent struct {
	Kind          EventKind
	TextDelta     string
	ThinkingDelta string
	Signature     string
	Data          string // redacted_thinking payload
	ToolCallID    string
	ToolName      string
	InputJSON     string
	StopReason    string
	Usage         *Usage
	Err           error
}

type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ThinkingMode selects how a request asks the model to think.
type ThinkingMode string

const (
	// ThinkingModeOff sends no thinking parameter. The empty mode means off.
	ThinkingModeOff ThinkingMode = "off"
	// ThinkingModeAdaptive lets the model decide how much to think.
	ThinkingModeAdaptive ThinkingMode = "adaptive"
	// ThinkingModeBudget enables thinking with a fixed token budget.
	ThinkingModeBudget ThinkingMode = "budget"
)

type Request struct {
	Model          string
	System         string
	Messages       []Message
	Tools          []ToolDef
	MaxTokens      int
	Thinking       ThinkingMode
	ThinkingBudget int // token budget, used only in ThinkingModeBudget
	// ReasoningEffort is passed through unchanged: reasoning_effort on the
	// OpenAI wire, output_config.effort on the Anthropic wire.
	ReasoningEffort string
}

type Client interface {
	Stream(ctx context.Context, req Request) (<-chan StreamEvent, error)
}
