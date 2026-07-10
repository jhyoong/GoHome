package openai

import (
	"encoding/json"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

func TestBuildOpenAIBody(t *testing.T) {
	req := common.Request{
		Model:  "gpt-4o",
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
						ToolUseID: "call_01",
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
						ToolUseID:  "call_01",
						ResultText: "file1.txt\nfile2.txt",
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

	data, err := buildOpenAIBody(req)
	if err != nil {
		t.Fatalf("buildOpenAIBody error: %v", err)
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
	if model != "gpt-4o" {
		t.Errorf("model: got %q, want gpt-4o", model)
	}

	// stream
	var stream bool
	if err := json.Unmarshal(body["stream"], &stream); err != nil {
		t.Fatalf("unmarshal stream: %v", err)
	}
	if !stream {
		t.Error("expected stream:true")
	}

	// stream_options.include_usage
	var streamOpts struct {
		IncludeUsage bool `json:"include_usage"`
	}
	if err := json.Unmarshal(body["stream_options"], &streamOpts); err != nil {
		t.Fatalf("unmarshal stream_options: %v", err)
	}
	if !streamOpts.IncludeUsage {
		t.Error("expected stream_options.include_usage:true")
	}

	// max_tokens
	var maxTokens int
	if err := json.Unmarshal(body["max_tokens"], &maxTokens); err != nil {
		t.Fatalf("unmarshal max_tokens: %v", err)
	}
	if maxTokens != 1024 {
		t.Errorf("max_tokens: got %d, want 1024", maxTokens)
	}

	// messages: system + user + assistant + tool = 4
	var messages []json.RawMessage
	if err := json.Unmarshal(body["messages"], &messages); err != nil {
		t.Fatalf("messages unmarshal: %v", err)
	}
	if len(messages) != 4 {
		t.Fatalf("expected 4 messages (system+user+assistant+tool), got %d", len(messages))
	}

	// message 0: system
	var msg0 struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(messages[0], &msg0); err != nil {
		t.Fatalf("unmarshal msg0: %v", err)
	}
	if msg0.Role != "system" {
		t.Errorf("msg0 role: got %q, want system", msg0.Role)
	}
	if msg0.Content != "You are a helpful assistant." {
		t.Errorf("msg0 content: got %q", msg0.Content)
	}

	// message 1: user with plain text content
	var msg1 struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(messages[1], &msg1); err != nil {
		t.Fatalf("unmarshal msg1: %v", err)
	}
	if msg1.Role != "user" {
		t.Errorf("msg1 role: got %q, want user", msg1.Role)
	}
	if msg1.Content != "What files are in /tmp?" {
		t.Errorf("msg1 content: got %q", msg1.Content)
	}

	// message 2: assistant with tool_calls
	var msg2 struct {
		Role      string `json:"role"`
		Content   interface{}
		ToolCalls []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	}
	if err := json.Unmarshal(messages[2], &msg2); err != nil {
		t.Fatalf("unmarshal msg2: %v", err)
	}
	if msg2.Role != "assistant" {
		t.Errorf("msg2 role: got %q, want assistant", msg2.Role)
	}
	if len(msg2.ToolCalls) != 1 {
		t.Fatalf("msg2 tool_calls len: got %d, want 1", len(msg2.ToolCalls))
	}
	tc := msg2.ToolCalls[0]
	if tc.ID != "call_01" {
		t.Errorf("tool_call id: got %q, want call_01", tc.ID)
	}
	if tc.Type != "function" {
		t.Errorf("tool_call type: got %q, want function", tc.Type)
	}
	if tc.Function.Name != "read_file" {
		t.Errorf("tool_call function.name: got %q, want read_file", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"path":"/tmp"}` {
		t.Errorf("tool_call function.arguments: got %q", tc.Function.Arguments)
	}

	// message 3: tool result
	var msg3 struct {
		Role       string `json:"role"`
		ToolCallID string `json:"tool_call_id"`
		Content    string `json:"content"`
	}
	if err := json.Unmarshal(messages[3], &msg3); err != nil {
		t.Fatalf("unmarshal msg3: %v", err)
	}
	if msg3.Role != "tool" {
		t.Errorf("msg3 role: got %q, want tool", msg3.Role)
	}
	if msg3.ToolCallID != "call_01" {
		t.Errorf("msg3 tool_call_id: got %q, want call_01", msg3.ToolCallID)
	}
	if msg3.Content != "file1.txt\nfile2.txt" {
		t.Errorf("msg3 content: got %q", msg3.Content)
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
		Type     string `json:"type"`
		Function struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		} `json:"function"`
	}
	if err := json.Unmarshal(tools[0], &tool0); err != nil {
		t.Fatalf("unmarshal tool0: %v", err)
	}
	if tool0.Type != "function" {
		t.Errorf("tool type: got %q, want function", tool0.Type)
	}
	if tool0.Function.Name != "read_file" {
		t.Errorf("tool function.name: got %q, want read_file", tool0.Function.Name)
	}
	if tool0.Function.Description != "Reads a file from disk." {
		t.Errorf("tool function.description: got %q", tool0.Function.Description)
	}
	if string(tool0.Function.Parameters) == "" {
		t.Error("tool function.parameters is empty")
	}
}

func TestTranslateAssistantMessage_ThinkingBlockDropped(t *testing.T) {
	// A session-resume scenario: assistant message contains a thinking block
	// followed by a text block. The OpenAI adapter must silently drop the
	// thinking block and only emit the text content.
	req := common.Request{
		Model: "gpt-4o",
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

	data, err := buildOpenAIBody(req)
	if err != nil {
		t.Fatalf("buildOpenAIBody error: %v", err)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	var messages []json.RawMessage
	if err := json.Unmarshal(body["messages"], &messages); err != nil {
		t.Fatalf("messages unmarshal: %v", err)
	}
	// user + assistant = 2 messages (no system)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	// message 1: assistant — content should only contain the text, not the thinking
	var msg1 struct {
		Role      string          `json:"role"`
		Content   *string         `json:"content"`
		ToolCalls json.RawMessage `json:"tool_calls"`
	}
	if err := json.Unmarshal(messages[1], &msg1); err != nil {
		t.Fatalf("unmarshal msg1: %v", err)
	}
	if msg1.Role != "assistant" {
		t.Errorf("msg1 role: got %q, want assistant", msg1.Role)
	}
	if msg1.Content == nil {
		t.Fatal("msg1 content is nil; expected text content")
	}
	if *msg1.Content != "The answer is 42." {
		t.Errorf("msg1 content: got %q, want %q", *msg1.Content, "The answer is 42.")
	}
}

func TestBuildOpenAIBody_MaxTokensZero(t *testing.T) {
	req := common.Request{
		Model:     "gpt-4o",
		Messages:  []common.Message{{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}}},
		MaxTokens: 0,
	}
	_, err := buildOpenAIBody(req)
	if err == nil {
		t.Fatal("expected error for MaxTokens=0")
	}
}

func TestBuildOpenAIBody_MaxTokensNegative(t *testing.T) {
	req := common.Request{
		Model:     "gpt-4o",
		Messages:  []common.Message{{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}}},
		MaxTokens: -1,
	}
	_, err := buildOpenAIBody(req)
	if err == nil {
		t.Fatal("expected error for MaxTokens=-1")
	}
}

func TestBuildOpenAIBody_NoTools(t *testing.T) {
	req := common.Request{
		Model:     "gpt-4o",
		Messages:  []common.Message{{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}}},
		MaxTokens: 100,
	}
	data, err := buildOpenAIBody(req)
	if err != nil {
		t.Fatalf("buildOpenAIBody error: %v", err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := body["tools"]; ok {
		t.Error("tools should be omitted when empty")
	}
}

func TestTranslateToolMessage_MultipleResults(t *testing.T) {
	// Regression: a single RoleTool message with multiple BlockToolResult blocks
	// (parallel tool calls) must produce one separate "tool"-role wire message
	// per result.
	req := common.Request{
		Model:  "gpt-4o",
		System: "You are a helpful assistant.",
		Messages: []common.Message{
			{
				Role: common.RoleUser,
				Content: []common.Block{
					{Kind: common.BlockText, Text: "Read both files."},
				},
			},
			{
				Role: common.RoleAssistant,
				Content: []common.Block{
					{
						Kind:      common.BlockToolUse,
						ToolUseID: "call_01",
						ToolName:  "read_file",
						InputJSON: `{"path":"/tmp/a.txt"}`,
					},
					{
						Kind:      common.BlockToolUse,
						ToolUseID: "call_02",
						ToolName:  "read_file",
						InputJSON: `{"path":"/tmp/b.txt"}`,
					},
				},
			},
			{
				Role: common.RoleTool,
				Content: []common.Block{
					{
						Kind:       common.BlockToolResult,
						ToolUseID:  "call_01",
						ResultText: "contents of a",
					},
					{
						Kind:       common.BlockToolResult,
						ToolUseID:  "call_02",
						ResultText: "contents of b",
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

	data, err := buildOpenAIBody(req)
	if err != nil {
		t.Fatalf("buildOpenAIBody error: %v", err)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	var messages []json.RawMessage
	if err := json.Unmarshal(body["messages"], &messages); err != nil {
		t.Fatalf("messages unmarshal: %v", err)
	}

	// Expect 5 wire messages: system + user + assistant + tool(call_01) + tool(call_02)
	if len(messages) != 5 {
		t.Fatalf("expected 5 messages (system+user+assistant+tool_01+tool_02), got %d", len(messages))
	}

	// message 3: first tool result
	var msg3 struct {
		Role       string `json:"role"`
		ToolCallID string `json:"tool_call_id"`
		Content    string `json:"content"`
	}
	if err := json.Unmarshal(messages[3], &msg3); err != nil {
		t.Fatalf("unmarshal msg3: %v", err)
	}
	if msg3.Role != "tool" {
		t.Errorf("msg3 role: got %q, want tool", msg3.Role)
	}
	if msg3.ToolCallID != "call_01" {
		t.Errorf("msg3 tool_call_id: got %q, want call_01", msg3.ToolCallID)
	}
	if msg3.Content != "contents of a" {
		t.Errorf("msg3 content: got %q, want %q", msg3.Content, "contents of a")
	}

	// message 4: second tool result
	var msg4 struct {
		Role       string `json:"role"`
		ToolCallID string `json:"tool_call_id"`
		Content    string `json:"content"`
	}
	if err := json.Unmarshal(messages[4], &msg4); err != nil {
		t.Fatalf("unmarshal msg4: %v", err)
	}
	if msg4.Role != "tool" {
		t.Errorf("msg4 role: got %q, want tool", msg4.Role)
	}
	if msg4.ToolCallID != "call_02" {
		t.Errorf("msg4 tool_call_id: got %q, want call_02", msg4.ToolCallID)
	}
	if msg4.Content != "contents of b" {
		t.Errorf("msg4 content: got %q, want %q", msg4.Content, "contents of b")
	}
}

func TestBuildOpenAIBody_NoSystem(t *testing.T) {
	req := common.Request{
		Model:     "gpt-4o",
		Messages:  []common.Message{{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}}},
		MaxTokens: 100,
	}
	data, err := buildOpenAIBody(req)
	if err != nil {
		t.Fatalf("buildOpenAIBody error: %v", err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var messages []json.RawMessage
	if err := json.Unmarshal(body["messages"], &messages); err != nil {
		t.Fatalf("messages unmarshal: %v", err)
	}
	// only the user message; no system prepended
	if len(messages) != 1 {
		t.Errorf("expected 1 message (no system), got %d", len(messages))
	}
}
