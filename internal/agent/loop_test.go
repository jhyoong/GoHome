package agent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/JiaHui/gohome/internal/agent"
	"github.com/JiaHui/gohome/internal/approval"
	"github.com/JiaHui/gohome/internal/config"
	"github.com/JiaHui/gohome/internal/llm"
	"github.com/JiaHui/gohome/internal/session"
	"github.com/JiaHui/gohome/internal/tools"
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
		nil,
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
		nil,
		steerCh,
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
