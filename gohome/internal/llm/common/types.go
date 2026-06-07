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
)

type Block struct {
	Kind       BlockKind `json:"kind"`
	Text       string    `json:"text,omitempty"`
	Signature  string    `json:"signature,omitempty"`
	ToolUseID  string    `json:"toolUseId,omitempty"`
	ToolName   string    `json:"toolName,omitempty"`
	InputJSON  string    `json:"inputJson,omitempty"`
	ResultText string    `json:"resultText,omitempty"`
	IsError    bool      `json:"isError,omitempty"`
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

type Request struct {
	Model          string
	System         string
	Messages       []Message
	Tools          []ToolDef
	MaxTokens      int
	ThinkingBudget int // if > 0, enable extended thinking with this token budget
}

type Client interface {
	Stream(ctx context.Context, req Request) (<-chan StreamEvent, error)
}
