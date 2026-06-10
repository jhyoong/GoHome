# H3: Fix OpenAI Adapter Dropping Parallel Tool Results — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix the OpenAI adapter so that all tool results from parallel tool calls are sent as separate `tool`-role wire messages, instead of silently dropping all but the first.

**Architecture:** Change `translateMessage` and its callees in the OpenAI adapter to return `[]any` instead of `any`. `translateToolMessage` expands one internal `RoleTool` message into N wire messages. The caller in `buildOpenAIBody` spreads the slice into the messages array. Anthropic adapter and agent internals are untouched.

**Tech Stack:** Go, `gohome/internal/llm/openai` package, standard `encoding/json` and `testing`.

---

## Task 1: Write the failing test for multi-result tool messages

**Files:**
- Modify: `gohome/internal/llm/openai/request_test.go` (append new test at end of file)

**Step 1: Write the failing test**

Add this test to the end of `request_test.go`:

```go
func TestTranslateToolMessage_MultipleResults(t *testing.T) {
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
				Description: "Reads a file.",
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

	// system + user + assistant + tool_01 + tool_02 = 5
	if len(messages) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(messages))
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
```

**Step 2: Run the test to verify it fails**

Run: `go test ./gohome/internal/llm/openai/ -run TestTranslateToolMessage_MultipleResults -v`

Expected: FAIL — the current code only emits one tool message, so `len(messages)` will be 4 instead of 5.

---

## Task 2: Change `translateMessage` return type to `[]any`

**Files:**
- Modify: `gohome/internal/llm/openai/request.go`

**Step 1: Update `translateMessage` signature and body**

Change lines 128-139 from:

```go
func translateMessage(m common.Message) (any, error) {
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
```

to:

```go
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
```

**Step 2: Update `translateUserMessage` return type**

Change lines 141-149 from:

```go
func translateUserMessage(m common.Message) (any, error) {
	var parts []string
	for _, b := range m.Content {
		if b.Kind != common.BlockText {
			return nil, fmt.Errorf("openai: unexpected block kind %q in user message", b.Kind)
		}
		parts = append(parts, b.Text)
	}
	return openaiUserMessage{Role: "user", Content: strings.Join(parts, "")}, nil
}
```

to:

```go
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
```

**Step 3: Update `translateAssistantMessage` return type**

Change lines 152-191 — only the signature and the final return. The function signature becomes:

```go
func translateAssistantMessage(m common.Message) ([]any, error) {
```

And the return at line 190 changes from:

```go
	return msg, nil
```

to:

```go
	return []any{msg}, nil
```

**Step 4: Rewrite `translateToolMessage` to iterate all blocks**

Replace lines 193-211 with:

```go
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
```

**Step 5: Update the caller in `buildOpenAIBody`**

Change lines 92-98 from:

```go
	for _, m := range req.Messages {
		msg, err := translateMessage(m)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, msg)
	}
```

to:

```go
	for _, m := range req.Messages {
		translated, err := translateMessage(m)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, translated...)
	}
```

**Step 6: Verify compilation**

Run: `go vet ./gohome/internal/llm/openai/`

Expected: clean (no errors).

---

## Task 3: Run all tests and verify

**Step 1: Run the new test**

Run: `go test ./gohome/internal/llm/openai/ -run TestTranslateToolMessage_MultipleResults -v`

Expected: PASS — 5 messages with both tool results present.

**Step 2: Run all OpenAI adapter tests**

Run: `go test ./gohome/internal/llm/openai/ -v`

Expected: all tests PASS. The existing `TestBuildOpenAIBody` (single tool result) still produces 4 messages and passes unchanged.

**Step 3: Run the full test suite**

Run: `go test ./gohome/...`

Expected: all packages PASS. No other package calls these functions.

**Step 4: Commit**

```bash
git add gohome/internal/llm/openai/request.go gohome/internal/llm/openai/request_test.go
git commit -m "fix(openai): emit one tool message per result instead of dropping all but first

translateMessage now returns []any so that a single RoleTool message
with N BlockToolResult blocks expands into N separate openaiToolMessage
wire messages. Previously only Content[0] was serialized, silently
dropping parallel tool results.

Fixes H3 from FABLE_REVIEW.md."
```

---

## Verification checklist

- [ ] New test fails before implementation (Task 1 step 2)
- [ ] `go vet` clean after implementation (Task 2 step 6)
- [ ] New test passes after implementation (Task 3 step 1)
- [ ] All existing OpenAI tests still pass (Task 3 step 2)
- [ ] Full suite passes (Task 3 step 3)
