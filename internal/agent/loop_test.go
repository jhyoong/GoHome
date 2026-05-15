package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jhyoong/gohome/internal/agent"
	"github.com/jhyoong/gohome/internal/approval"
	"github.com/jhyoong/gohome/internal/config"
	"github.com/jhyoong/gohome/internal/llm"
	"github.com/jhyoong/gohome/internal/session"
	"github.com/jhyoong/gohome/internal/tools"
)

// mockTool is a minimal Tool implementation for tests.
type mockTool struct{ execCount int }

func (m *mockTool) Name() string                   { return "test_tool" }
func (m *mockTool) Description() string            { return "a test tool" }
func (m *mockTool) Parameters() json.RawMessage    { return json.RawMessage(`{}`) }
func (m *mockTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	m.execCount++
	return "tool output", nil
}

// sseToolCall returns SSE bytes for a single tool call response.
func sseToolCall() string {
	return strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"tc1","type":"function","function":{"name":"test_tool","arguments":"{}"}}]},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
		"",
	}, "\n\n")
}

// sseText returns SSE bytes for a simple text response.
func sseText(content string) string {
	return strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"` + content + `"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
		"",
	}, "\n\n")
}

func sseTextWithUsage(content string, prompt, completion, total int) string {
	usageJSON := fmt.Sprintf(`{"prompt_tokens":%d,"completion_tokens":%d,"total_tokens":%d}`, prompt, completion, total)
	return strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"` + content + `"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: {"choices":[],"usage":` + usageJSON + `}`,
		`data: [DONE]`,
		"",
	}, "\n\n")
}

func TestLoopUsageForwarded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseTextWithUsage("hello", 20, 3, 23)))
	}))
	defer srv.Close()

	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()
	sess, _ := store.CreateSession(ctx)

	loop := agent.NewLoop(llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"}), tools.NewRegistry(), store, "")
	broker := approval.NewBroker(config.ApprovalConfig{}, nil)

	var gotPrompt, gotCompletion, gotTotal int
	err := loop.Run(ctx, sess.ID, "tab-1", "hello", broker,
		func(tok string) {},
		func(msg string) {},
		nil, // onToolResult
		nil, // steerCh
		func(prompt, completion, total int) {
			gotPrompt = prompt
			gotCompletion = completion
			gotTotal = total
		},
		nil, // onThinking
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotPrompt != 20 || gotCompletion != 3 || gotTotal != 23 {
		t.Errorf("usage: got prompt=%d completion=%d total=%d, want 20 3 23", gotPrompt, gotCompletion, gotTotal)
	}
}

func TestSimpleMessageRoundtrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseText("world")))
	}))
	defer srv.Close()

	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()
	sess, _ := store.CreateSession(ctx)

	llmClient := llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"})
	reg := tools.NewRegistry()
	broker := approval.NewBroker(config.ApprovalConfig{}, nil)
	loop := agent.NewLoop(llmClient, reg, store, "")

	var tokens []string
	err := loop.Run(ctx, sess.ID, "tab-1", "hello", broker,
		func(tok string) { tokens = append(tokens, tok) },
		func(msg string) {},
		nil, // onToolResult
		nil, // steerCh
		nil, // onUsage
		nil, // onThinking
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	result := strings.Join(tokens, "")
	if result != "world" {
		t.Errorf("got %q, want %q", result, "world")
	}

	msgs, _ := store.GetMessages(ctx, sess.ID)
	if len(msgs) < 1 || msgs[0].Content != "hello" {
		t.Errorf("user message not persisted: %+v", msgs)
	}
}

func TestOnToolResultCallback(t *testing.T) {
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		n := atomic.AddInt32(&callCount, 1)
		if n == 1 {
			w.Write([]byte(sseToolCall()))
		} else {
			w.Write([]byte(sseText("done")))
		}
	}))
	defer srv.Close()

	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()
	sess, _ := store.CreateSession(ctx)

	reg := tools.NewRegistry()
	mt := &mockTool{}
	reg.Register(mt)

	broker := approval.NewBroker(config.ApprovalConfig{AutoApproveAll: true}, nil)
	loop := agent.NewLoop(llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"}), reg, store, "")

	var gotTool, gotParams, gotResult string
	var gotApproved bool
	err := loop.Run(ctx, sess.ID, "tab-1", "run tool", broker,
		func(tok string) {},
		func(msg string) {},
		func(tool, params, result string, approved bool) {
			gotTool = tool
			gotParams = params
			gotResult = result
			gotApproved = approved
		},
		nil, // steerCh
		nil, // onUsage
		nil, // onThinking
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotTool != "test_tool" {
		t.Errorf("onToolResult tool: got %q, want %q", gotTool, "test_tool")
	}
	if gotParams != "{}" {
		t.Errorf("onToolResult params: got %q, want %q", gotParams, "{}")
	}
	if gotResult != "tool output" {
		t.Errorf("onToolResult result: got %q, want %q", gotResult, "tool output")
	}
	if !gotApproved {
		t.Errorf("onToolResult approved: want true")
	}
	if mt.execCount != 1 {
		t.Errorf("tool executed %d times, want 1", mt.execCount)
	}
}

func TestGenerateTitle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if len(body.Messages) != 2 {
			t.Errorf("want 2 messages, got %d", len(body.Messages))
		}
		if len(body.Messages) >= 1 && body.Messages[0].Role != "system" {
			t.Errorf("first message role: got %q, want %q", body.Messages[0].Role, "system")
		}
		if len(body.Messages) >= 2 {
			if body.Messages[1].Role != "user" {
				t.Errorf("second message role: got %q, want %q", body.Messages[1].Role, "user")
			}
			if body.Messages[1].Content != "find all files in the current directory" {
				t.Errorf("second message content: got %q", body.Messages[1].Content)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseText("  Find Files in Directory  ")))
	}))
	defer srv.Close()

	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()

	loop := agent.NewLoop(
		llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"}),
		tools.NewRegistry(), store, "",
	)

	title, err := loop.GenerateTitle(context.Background(), "find all files in the current directory")
	if err != nil {
		t.Fatalf("GenerateTitle: %v", err)
	}
	if title != "Find Files in Directory" {
		t.Errorf("got %q, want %q", title, "Find Files in Directory")
	}
}

func TestGenerateTitleEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseText("   ")))
	}))
	defer srv.Close()

	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()

	loop := agent.NewLoop(
		llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"}),
		tools.NewRegistry(), store, "",
	)

	_, err := loop.GenerateTitle(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for empty LLM response")
	}
}

func TestGenerateTitleLLMError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()

	loop := agent.NewLoop(
		llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"}),
		tools.NewRegistry(), store, "",
	)

	_, err := loop.GenerateTitle(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error when LLM returns non-200")
	}
}

func TestSteeringMessageInjected(t *testing.T) {
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		n := atomic.AddInt32(&callCount, 1)
		if n == 1 {
			w.Write([]byte(sseToolCall()))
		} else {
			w.Write([]byte(sseText("ok")))
		}
	}))
	defer srv.Close()

	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()
	sess, _ := store.CreateSession(ctx)

	reg := tools.NewRegistry()
	reg.Register(&mockTool{})
	broker := approval.NewBroker(config.ApprovalConfig{AutoApproveAll: true}, nil)
	loop := agent.NewLoop(llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"}), reg, store, "")

	steerCh := make(chan string, 4)
	steerCh <- "please stop and summarize" // pre-populated; drained before second LLM call

	err := loop.Run(ctx, sess.ID, "tab-1", "hello", broker,
		func(tok string) {},
		func(msg string) {},
		nil, // onToolResult
		steerCh,
		nil, // onUsage
		nil, // onThinking
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	msgs, _ := store.GetMessages(ctx, sess.ID)
	var found bool
	for _, m := range msgs {
		if m.Role == "user" && m.Content == "please stop and summarize" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("steering message not saved to DB; got messages: %+v", msgs)
	}
}

// sseTextWithPredictions returns SSE bytes simulating a response with reasoning_content followed by content.
func sseTextWithPredictions(content, reasoning string) string {
	return strings.Join([]string{
		`data: {"choices":[{"delta":{"reasoning_content":"` + reasoning + `"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"content":"` + content + `"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
		"",
	}, "\n\n")
}

// TestThinkingCallbackCalled verifies that onThinking is called with prediction content during streaming.
func TestThinkingCallbackCalled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseTextWithPredictions("hello world", "I need to think about this")))
	}))
	defer srv.Close()

	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()
	sess, _ := store.CreateSession(ctx)

	loop := agent.NewLoop(llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"}), tools.NewRegistry(), store, "")
	broker := approval.NewBroker(config.ApprovalConfig{}, nil)

	var thinkingContent []string
	err := loop.Run(ctx, sess.ID, "tab-1", "hello", broker,
		func(tok string) {},
		func(msg string) {},
		nil, // onToolResult
		nil, // steerCh
		nil, // onUsage
		func(prediction string) {
			thinkingContent = append(thinkingContent, prediction)
		},
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(thinkingContent) == 0 {
		t.Error("onThinking callback was not called - expected prediction content to be received")
	}
	if len(thinkingContent) > 0 && thinkingContent[0] != "I need to think about this" {
		t.Errorf("onThinking content: got %q, want %q", thinkingContent[0], "I need to think about this")
	}
}

// TestAssistantMessageWithThinkingSaved verifies that assistant message is saved with Thinking field populated.
func TestAssistantMessageWithThinkingSaved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseTextWithPredictions("This is my response", "my internal reasoning")))
	}))
	defer srv.Close()

	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()
	sess, _ := store.CreateSession(ctx)

	loop := agent.NewLoop(llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"}), tools.NewRegistry(), store, "")
	broker := approval.NewBroker(config.ApprovalConfig{}, nil)

	var thinkingContent string
	err := loop.Run(ctx, sess.ID, "tab-1", "hello", broker,
		func(tok string) {},
		func(msg string) {},
		nil, // onToolResult
		nil, // steerCh
		nil, // onUsage
		func(prediction string) {
			thinkingContent = prediction
		},
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify assistant message was saved with Thinking field populated
	msgs, err := store.GetMessages(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}

	var assistantWithThinking bool
	for _, m := range msgs {
		if m.Role == "assistant" && m.Content == "This is my response" {
			if m.Thinking == "" {
				t.Errorf("Thinking field is empty - expected Thinking=%q", thinkingContent)
			} else if m.Thinking != thinkingContent {
				t.Errorf("Thinking field: got %q, want %q", m.Thinking, thinkingContent)
			} else {
				assistantWithThinking = true
			}
		}
	}
	if !assistantWithThinking {
		t.Error("assistant message with thinking content not saved correctly")
	}
}

// TestAssistantMessageWithEmptyThinkingSaved verifies that assistant message is saved with empty Thinking field.
func TestAssistantMessageWithEmptyThinkingSaved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// SSE with no predictions - simulating response without thinking content
		w.Write([]byte(strings.Join([]string{
			`data: {"choices":[{"delta":{"content":"simple response"},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			`data: [DONE]`,
			"",
		}, "\n\n")))
	}))
	defer srv.Close()

	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()
	sess, _ := store.CreateSession(ctx)

	loop := agent.NewLoop(llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"}), tools.NewRegistry(), store, "")
	broker := approval.NewBroker(config.ApprovalConfig{}, nil)

	err := loop.Run(ctx, sess.ID, "tab-1", "hello", broker,
		func(tok string) {},
		func(msg string) {},
		nil, // onToolResult
		nil, // steerCh
		nil, // onUsage
		nil, // onThinking - no predictions callback
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify assistant message was saved with empty Thinking field
	msgs, err := store.GetMessages(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}

	var found bool
	for _, m := range msgs {
		if m.Role == "assistant" && m.Content == "simple response" {
			// Verify Thinking field is empty string (not nil/omitted)
			if m.Thinking != "" {
				t.Errorf("Thinking field should be empty string when no thinking content, got: %q", m.Thinking)
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("assistant message not saved correctly")
	}
}
