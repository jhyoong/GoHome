package openai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// openaiToolFunction is the function spec inside an OpenAI tool definition.
type openaiToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// openaiTool is the OpenAI wire shape for a tool definition.
type openaiTool struct {
	Type     string             `json:"type"`
	Function openaiToolFunction `json:"function"`
}

// openaiToolCallFunction is the function spec inside a tool_call.
type openaiToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// openaiToolCall is the OpenAI wire shape for a tool_call in an assistant message.
type openaiToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function openaiToolCallFunction `json:"function"`
}

// openaiSystemMessage is an OpenAI system-role message.
type openaiSystemMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openaiUserMessage is an OpenAI user-role message.
type openaiUserMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openaiAssistantMessage is an OpenAI assistant-role message.
// Content may be null when tool_calls are present; use *string so we can set null.
type openaiAssistantMessage struct {
	Role      string           `json:"role"`
	Content   *string          `json:"content"`
	ToolCalls []openaiToolCall `json:"tool_calls,omitempty"`
}

// openaiToolMessage is an OpenAI tool-role message.
type openaiToolMessage struct {
	Role       string `json:"role"`
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
}

// openaiStreamOptions requests usage in the final stream chunk.
type openaiStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// openaiBody is the OpenAI wire shape for a chat completions request body.
type openaiBody struct {
	Model         string              `json:"model"`
	Messages      []any               `json:"messages"`
	Tools         []openaiTool        `json:"tools,omitempty"`
	MaxTokens     int                 `json:"max_tokens"`
	Stream        bool                `json:"stream"`
	StreamOptions openaiStreamOptions `json:"stream_options"`
}

// buildOpenAIBody translates a common.Request to OpenAI chat completions wire-format JSON.
func buildOpenAIBody(req common.Request) ([]byte, error) {
	if req.MaxTokens <= 0 {
		return nil, fmt.Errorf("openai: max_tokens must be > 0")
	}

	var msgs []any

	// Prepend system message if present.
	if req.System != "" {
		msgs = append(msgs, openaiSystemMessage{Role: "system", Content: req.System})
	}

	for _, m := range req.Messages {
		translated, err := translateMessage(m)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, translated...)
	}

	tools := make([]openaiTool, 0, len(req.Tools))
	for _, t := range req.Tools {
		tools = append(tools, openaiTool{
			Type: "function",
			Function: openaiToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}

	body := openaiBody{
		Model:     req.Model,
		Messages:  msgs,
		MaxTokens: req.MaxTokens,
		Stream:    true,
		StreamOptions: openaiStreamOptions{
			IncludeUsage: true,
		},
	}
	if len(tools) > 0 {
		body.Tools = tools
	}

	return json.Marshal(body)
}

func translateMessage(m common.Message) ([]any, error) {
	switch m.Role {
	case common.RoleUser:
		return translateUserMessage(m)
	case common.RoleAssistant:
		return translateAssistantMessage(m)
	case common.RoleTool:
		return translateToolMessage(m)
	default:
		return nil, fmt.Errorf("openai: unknown role %q", m.Role)
	}
}

func translateUserMessage(m common.Message) ([]any, error) {
	var parts []string
	for _, b := range m.Content {
		if b.Kind != common.BlockText {
			return nil, fmt.Errorf("openai: unexpected block kind %q in user message", b.Kind)
		}
		parts = append(parts, b.Text)
	}
	return []any{openaiUserMessage{Role: "user", Content: strings.Join(parts, "")}}, nil
}

func translateAssistantMessage(m common.Message) ([]any, error) {
	var textParts []string
	var toolCalls []openaiToolCall

	for _, b := range m.Content {
		switch b.Kind {
		case common.BlockText:
			textParts = append(textParts, b.Text)
		case common.BlockThinking:
			// OpenAI does not support thinking blocks; skip without emitting.
			// The thinking content is preserved in session history but not sent to OpenAI.
		case common.BlockToolUse:
			args := b.InputJSON
			if args == "" {
				args = "{}"
			}
			toolCalls = append(toolCalls, openaiToolCall{
				ID:   b.ToolUseID,
				Type: "function",
				Function: openaiToolCallFunction{
					Name:      b.ToolName,
					Arguments: args,
				},
			})
		default:
			return nil, fmt.Errorf("openai: unexpected block kind %q in assistant message", b.Kind)
		}
	}

	msg := openaiAssistantMessage{
		Role:      "assistant",
		ToolCalls: toolCalls,
	}
	if len(textParts) > 0 {
		s := strings.Join(textParts, "")
		msg.Content = &s
	}
	// content is nil (null in JSON) when only tool_calls present
	return []any{msg}, nil
}

func translateToolMessage(m common.Message) ([]any, error) {
	if len(m.Content) == 0 {
		return nil, fmt.Errorf("openai: tool message has no content blocks")
	}
	var msgs []any
	for _, b := range m.Content {
		if b.Kind != common.BlockToolResult {
			return nil, fmt.Errorf("openai: unexpected block kind %q in tool message", b.Kind)
		}
		msgs = append(msgs, openaiToolMessage{
			Role:       "tool",
			ToolCallID: b.ToolUseID,
			Content:    b.ResultText,
		})
	}
	return msgs, nil
}
