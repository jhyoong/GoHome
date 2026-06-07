package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

func TestBuildAnthropicBody(t *testing.T) {
	req := common.Request{
		Model:  "claude-3-5-haiku-20241022",
		System: "You are a helpful assistant.",
		Messages: []common.Message{
			{
				Role: common.RoleUser,
				Content: []common.Block{
					{Kind: common.BlockText, Text: "What files are in /tmp?"},
				},
			},
			{
				Role: common.RoleAssistant,
				Content: []common.Block{
					{
						Kind:      common.BlockToolUse,
						ToolUseID: "toolu_01",
						ToolName:  "read_file",
						InputJSON: `{"path":"/tmp"}`,
					},
				},
			},
			{
				Role: common.RoleTool,
				Content: []common.Block{
					{
						Kind:       common.BlockToolResult,
						ToolUseID:  "toolu_01",
						ResultText: "file1.txt\nfile2.txt",
						IsError:    false,
					},
				},
			},
		},
		Tools: []common.ToolDef{
			{
				Name:        "read_file",
				Description: "Reads a file from disk.",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
			},
		},
		MaxTokens: 1024,
	}

	data, err := buildAnthropicBody(req)
	if err != nil {
		t.Fatalf("buildAnthropicBody error: %v", err)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	// model
	var model string
	if err := json.Unmarshal(body["model"], &model); err != nil {
		t.Fatalf("unmarshal model: %v", err)
	}
	if model != "claude-3-5-haiku-20241022" {
		t.Errorf("model: got %q", model)
	}

	// system
	var system string
	if err := json.Unmarshal(body["system"], &system); err != nil {
		t.Fatalf("unmarshal system: %v", err)
	}
	if system != "You are a helpful assistant." {
		t.Errorf("system: got %q", system)
	}

	// stream
	var stream bool
	if err := json.Unmarshal(body["stream"], &stream); err != nil {
		t.Fatalf("unmarshal stream: %v", err)
	}
	if !stream {
		t.Error("expected stream:true")
	}

	// max_tokens
	var maxTokens int
	if err := json.Unmarshal(body["max_tokens"], &maxTokens); err != nil {
		t.Fatalf("unmarshal max_tokens: %v", err)
	}
	if maxTokens != 1024 {
		t.Errorf("max_tokens: got %d", maxTokens)
	}

	// messages: expect 3 Anthropic messages (user, assistant, user wrapping tool_result)
	var messages []json.RawMessage
	if err := json.Unmarshal(body["messages"], &messages); err != nil {
		t.Fatalf("messages unmarshal: %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}

	// message 0: user with text block
	var msg0 struct {
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(messages[0], &msg0); err != nil {
		t.Fatalf("unmarshal msg0: %v", err)
	}
	if msg0.Role != "user" {
		t.Errorf("msg0 role: %q", msg0.Role)
	}
	if len(msg0.Content) != 1 {
		t.Fatalf("msg0 content len: %d", len(msg0.Content))
	}
	var block0 struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(msg0.Content[0], &block0); err != nil {
		t.Fatalf("unmarshal block0: %v", err)
	}
	if block0.Type != "text" || block0.Text != "What files are in /tmp?" {
		t.Errorf("msg0 block: type=%q text=%q", block0.Type, block0.Text)
	}

	// message 1: assistant with tool_use block
	var msg1 struct {
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(messages[1], &msg1); err != nil {
		t.Fatalf("unmarshal msg1: %v", err)
	}
	if msg1.Role != "assistant" {
		t.Errorf("msg1 role: %q", msg1.Role)
	}
	var block1 struct {
		Type  string          `json:"type"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(msg1.Content[0], &block1); err != nil {
		t.Fatalf("unmarshal block1: %v", err)
	}
	if block1.Type != "tool_use" || block1.ID != "toolu_01" || block1.Name != "read_file" {
		t.Errorf("msg1 tool_use block: type=%q id=%q name=%q", block1.Type, block1.ID, block1.Name)
	}
	var input map[string]string
	if err := json.Unmarshal(block1.Input, &input); err != nil {
		t.Fatalf("unmarshal block1 input: %v", err)
	}
	if input["path"] != "/tmp" {
		t.Errorf("msg1 tool_use input path: %q", input["path"])
	}

	// message 2: user wrapping tool_result
	var msg2 struct {
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(messages[2], &msg2); err != nil {
		t.Fatalf("unmarshal msg2: %v", err)
	}
	if msg2.Role != "user" {
		t.Errorf("msg2 role: %q", msg2.Role)
	}
	var block2 struct {
		Type      string `json:"type"`
		ToolUseID string `json:"tool_use_id"`
		Content   string `json:"content"`
		IsError   bool   `json:"is_error"`
	}
	if err := json.Unmarshal(msg2.Content[0], &block2); err != nil {
		t.Fatalf("unmarshal block2: %v", err)
	}
	if block2.Type != "tool_result" || block2.ToolUseID != "toolu_01" || block2.Content != "file1.txt\nfile2.txt" {
		t.Errorf("msg2 tool_result: type=%q id=%q content=%q", block2.Type, block2.ToolUseID, block2.Content)
	}

	// tools
	var tools []json.RawMessage
	if err := json.Unmarshal(body["tools"], &tools); err != nil {
		t.Fatalf("tools unmarshal: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	var tool0 struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	if err := json.Unmarshal(tools[0], &tool0); err != nil {
		t.Fatalf("unmarshal tool0: %v", err)
	}
	if tool0.Name != "read_file" || tool0.Description != "Reads a file from disk." {
		t.Errorf("tool0: name=%q desc=%q", tool0.Name, tool0.Description)
	}
	if string(tool0.InputSchema) == "" {
		t.Error("tool0 input_schema empty")
	}
}

func TestBuildAnthropicBody_Thinking(t *testing.T) {
	req := common.Request{
		Model:          "claude-sonnet-4-20250514",
		Messages:       []common.Message{{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}}},
		MaxTokens:      16384,
		ThinkingBudget: 10240,
	}

	data, err := buildAnthropicBody(req)
	if err != nil {
		t.Fatalf("buildAnthropicBody error: %v", err)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	raw, ok := body["thinking"]
	if !ok {
		t.Fatal("thinking field missing from request body")
	}
	var thinking struct {
		Type         string `json:"type"`
		BudgetTokens int    `json:"budget_tokens"`
	}
	if err := json.Unmarshal(raw, &thinking); err != nil {
		t.Fatalf("unmarshal thinking: %v", err)
	}
	if thinking.Type != "enabled" {
		t.Errorf("thinking.type: got %q, want %q", thinking.Type, "enabled")
	}
	if thinking.BudgetTokens != 10240 {
		t.Errorf("thinking.budget_tokens: got %d, want %d", thinking.BudgetTokens, 10240)
	}
}

func TestBuildAnthropicBody_NoThinkingWhenZeroBudget(t *testing.T) {
	req := common.Request{
		Model:     "claude-sonnet-4-20250514",
		Messages:  []common.Message{{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}}},
		MaxTokens: 4096,
	}

	data, err := buildAnthropicBody(req)
	if err != nil {
		t.Fatalf("buildAnthropicBody error: %v", err)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if _, ok := body["thinking"]; ok {
		t.Error("thinking field should be omitted when budget is zero")
	}
}

func TestTranslateAssistantMessage_ThinkingBlock(t *testing.T) {
	// A session-resume scenario: assistant message contains a thinking block
	// followed by a text block. The Anthropic adapter must emit both as content
	// blocks in the correct order.
	req := common.Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []common.Message{
			{
				Role: common.RoleUser,
				Content: []common.Block{
					{Kind: common.BlockText, Text: "Think about it."},
				},
			},
			{
				Role: common.RoleAssistant,
				Content: []common.Block{
					{Kind: common.BlockThinking, Text: "internal reasoning here"},
					{Kind: common.BlockText, Text: "The answer is 42."},
				},
			},
		},
		MaxTokens: 1024,
	}

	data, err := buildAnthropicBody(req)
	if err != nil {
		t.Fatalf("buildAnthropicBody error: %v", err)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	var messages []json.RawMessage
	if err := json.Unmarshal(body["messages"], &messages); err != nil {
		t.Fatalf("messages unmarshal: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	// message 1: assistant, should have 2 content blocks: thinking then text
	var msg1 struct {
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(messages[1], &msg1); err != nil {
		t.Fatalf("unmarshal msg1: %v", err)
	}
	if msg1.Role != "assistant" {
		t.Errorf("msg1 role: got %q, want assistant", msg1.Role)
	}
	if len(msg1.Content) != 2 {
		t.Fatalf("msg1 content blocks: got %d, want 2", len(msg1.Content))
	}

	// first block: thinking
	var thinkBlock struct {
		Type     string `json:"type"`
		Thinking string `json:"thinking"`
	}
	if err := json.Unmarshal(msg1.Content[0], &thinkBlock); err != nil {
		t.Fatalf("unmarshal thinking block: %v", err)
	}
	if thinkBlock.Type != "thinking" {
		t.Errorf("thinking block type: got %q, want thinking", thinkBlock.Type)
	}
	if thinkBlock.Thinking != "internal reasoning here" {
		t.Errorf("thinking block content: got %q", thinkBlock.Thinking)
	}

	// second block: text
	var textBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(msg1.Content[1], &textBlock); err != nil {
		t.Fatalf("unmarshal text block: %v", err)
	}
	if textBlock.Type != "text" {
		t.Errorf("text block type: got %q, want text", textBlock.Type)
	}
	if textBlock.Text != "The answer is 42." {
		t.Errorf("text block content: got %q", textBlock.Text)
	}
}

func TestBuildAnthropicBody_MaxTokensZero(t *testing.T) {
	req := common.Request{
		Model:     "claude-3-5-haiku-20241022",
		Messages:  []common.Message{{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}}},
		MaxTokens: 0,
	}
	_, err := buildAnthropicBody(req)
	if err == nil {
		t.Fatal("expected error for MaxTokens=0")
	}
}

func TestBuildAnthropicBody_MaxTokensNegative(t *testing.T) {
	req := common.Request{
		Model:     "claude-3-5-haiku-20241022",
		Messages:  []common.Message{{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}}},
		MaxTokens: -1,
	}
	_, err := buildAnthropicBody(req)
	if err == nil {
		t.Fatal("expected error for MaxTokens=-1")
	}
}
