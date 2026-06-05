package anthropic

import (
	"encoding/json"
	"fmt"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// anthropicTool is the Anthropic wire shape for a tool definition.
type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// anthropicTextBlock is the Anthropic wire shape for a text content block.
type anthropicTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// anthropicToolUseBlock is the Anthropic wire shape for a tool_use content block.
type anthropicToolUseBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// anthropicToolResultBlock is the Anthropic wire shape for a tool_result content block.
type anthropicToolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

// anthropicMessage is the Anthropic wire shape for a single message.
type anthropicMessage struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

// anthropicBody is the Anthropic wire shape for a messages request body.
type anthropicBody struct {
	Model     string             `json:"model"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
	MaxTokens int                `json:"max_tokens"`
	Stream    bool               `json:"stream"`
}

// buildAnthropicBody translates a common.Request to Anthropic wire-format JSON.
func buildAnthropicBody(req common.Request) ([]byte, error) {
	if req.MaxTokens <= 0 {
		return nil, fmt.Errorf("anthropic: max_tokens must be > 0")
	}

	msgs, err := translateMessages(req.Messages)
	if err != nil {
		return nil, err
	}

	tools := make([]anthropicTool, 0, len(req.Tools))
	for _, t := range req.Tools {
		tools = append(tools, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}

	body := anthropicBody{
		Model:     req.Model,
		System:    req.System,
		Messages:  msgs,
		MaxTokens: req.MaxTokens,
		Stream:    true,
	}
	if len(tools) > 0 {
		body.Tools = tools
	}

	return json.Marshal(body)
}

// translateMessages converts common.Message slice to Anthropic wire messages.
// RoleTool messages become user-role messages containing tool_result blocks.
func translateMessages(msgs []common.Message) ([]anthropicMessage, error) {
	result := make([]anthropicMessage, 0, len(msgs))
	for _, m := range msgs {
		am, err := translateMessage(m)
		if err != nil {
			return nil, err
		}
		result = append(result, am)
	}
	return result, nil
}

func translateMessage(m common.Message) (anthropicMessage, error) {
	switch m.Role {
	case common.RoleTool:
		return translateToolMessage(m)
	case common.RoleAssistant:
		return translateAssistantMessage(m)
	default:
		return translateUserMessage(m)
	}
}

func translateUserMessage(m common.Message) (anthropicMessage, error) {
	content := make([]any, 0, len(m.Content))
	for _, b := range m.Content {
		switch b.Kind {
		case common.BlockText:
			content = append(content, anthropicTextBlock{Type: "text", Text: b.Text})
		default:
			return anthropicMessage{}, fmt.Errorf("unexpected block kind %q in user message", b.Kind)
		}
	}
	return anthropicMessage{Role: "user", Content: content}, nil
}

func translateAssistantMessage(m common.Message) (anthropicMessage, error) {
	content := make([]any, 0, len(m.Content))
	for _, b := range m.Content {
		switch b.Kind {
		case common.BlockText:
			content = append(content, anthropicTextBlock{Type: "text", Text: b.Text})
		case common.BlockToolUse:
			var inputRaw json.RawMessage
			if b.InputJSON != "" {
				inputRaw = json.RawMessage(b.InputJSON)
			} else {
				inputRaw = json.RawMessage("{}")
			}
			content = append(content, anthropicToolUseBlock{
				Type:  "tool_use",
				ID:    b.ToolUseID,
				Name:  b.ToolName,
				Input: inputRaw,
			})
		default:
			return anthropicMessage{}, fmt.Errorf("unexpected block kind %q in assistant message", b.Kind)
		}
	}
	return anthropicMessage{Role: "assistant", Content: content}, nil
}

func translateToolMessage(m common.Message) (anthropicMessage, error) {
	content := make([]any, 0, len(m.Content))
	for _, b := range m.Content {
		if b.Kind != common.BlockToolResult {
			return anthropicMessage{}, fmt.Errorf("unexpected block kind %q in tool message", b.Kind)
		}
		content = append(content, anthropicToolResultBlock{
			Type:      "tool_result",
			ToolUseID: b.ToolUseID,
			Content:   b.ResultText,
			IsError:   b.IsError,
		})
	}
	return anthropicMessage{Role: "user", Content: content}, nil
}
