package agent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jhyoong/gohome/internal/agent"
	"github.com/jhyoong/gohome/internal/approval"
	"github.com/jhyoong/gohome/internal/config"
	"github.com/jhyoong/gohome/internal/llm"
	"github.com/jhyoong/gohome/internal/session"
	"github.com/jhyoong/gohome/internal/tools"
)

// mockSubagentEvents records calls to each SubagentEvents method.
type mockSubagentEvents struct {
	starts     []string
	dones      []string
	errors     []string
	tokens     []string
	toolResult int
}

func (m *mockSubagentEvents) OnStart(sessionID, _, _ string)                         { m.starts = append(m.starts, sessionID) }
func (m *mockSubagentEvents) OnToken(_, token string)                                { m.tokens = append(m.tokens, token) }
func (m *mockSubagentEvents) OnThinkingToken(_, _ string)                            {}
func (m *mockSubagentEvents) OnToolResult(_, _, _, _ string, _ bool)                 { m.toolResult++ }
func (m *mockSubagentEvents) OnDone(sessionID, _ string)                             { m.dones = append(m.dones, sessionID) }
func (m *mockSubagentEvents) OnError(sessionID, msg string)                          { m.errors = append(m.errors, msg) }

// TestSpawnSubagentTool_EmptyTask verifies that Execute returns an error when
// the task parameter is an empty string.
func TestSpawnSubagentTool_EmptyTask(t *testing.T) {
	store, err := session.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	sess, _ := store.CreateSession(ctx)

	broker := approval.NewBroker(config.ApprovalConfig{}, nil)
	events := &mockSubagentEvents{}

	tool := agent.NewSpawnSubagentTool(
		llm.NewClient(config.EndpointConfig{URL: "http://127.0.0.1:0", Model: "test"}),
		tools.NewRegistry(),
		store,
		broker,
		events,
		"",
		sess.ID,
	)

	params, _ := json.Marshal(map[string]string{"task": ""})
	_, err = tool.Execute(ctx, params)
	if err == nil {
		t.Fatal("expected error for empty task, got nil")
	}
	if err.Error() != "task is required" {
		t.Errorf("error message: got %q, want %q", err.Error(), "task is required")
	}
	if len(events.starts) != 0 {
		t.Errorf("OnStart should not be called on empty task, was called %d time(s)", len(events.starts))
	}
}

// TestSpawnSubagentTool_InvalidJSON verifies that Execute returns an error when
// the params JSON is malformed.
func TestSpawnSubagentTool_InvalidJSON(t *testing.T) {
	store, err := session.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	sess, _ := store.CreateSession(ctx)

	broker := approval.NewBroker(config.ApprovalConfig{}, nil)
	events := &mockSubagentEvents{}

	tool := agent.NewSpawnSubagentTool(
		llm.NewClient(config.EndpointConfig{URL: "http://127.0.0.1:0", Model: "test"}),
		tools.NewRegistry(),
		store,
		broker,
		events,
		"",
		sess.ID,
	)

	_, err = tool.Execute(ctx, json.RawMessage(`{bad json`))
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

// TestSpawnSubagentTool_BadParentID verifies that Execute returns an error when
// the parent session ID is invalid, causing CreateChildSession to fail with a
// foreign-key or "not found" error.
func TestSpawnSubagentTool_BadParentID(t *testing.T) {
	store, err := session.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	broker := approval.NewBroker(config.ApprovalConfig{}, nil)
	events := &mockSubagentEvents{}

	tool := agent.NewSpawnSubagentTool(
		llm.NewClient(config.EndpointConfig{URL: "http://127.0.0.1:0", Model: "test"}),
		tools.NewRegistry(),
		store,
		broker,
		events,
		"",
		"nonexistent-parent-id", // parent does not exist in the store
	)

	params, _ := json.Marshal(map[string]string{"task": "do something"})
	_, err = tool.Execute(ctx, params)
	if err == nil {
		t.Fatal("expected error from Execute with bad parent ID, got nil")
	}
	if len(events.starts) != 0 {
		t.Errorf("OnStart should not be called when CreateChildSession fails, was called %d time(s)", len(events.starts))
	}
}

// TestSpawnSubagentTool_HappyPath exercises Execute with a real in-memory store
// and a mock HTTP LLM server that returns a simple text response.
// It verifies that OnStart and OnDone are fired and that the final text is returned.
func TestSpawnSubagentTool_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseText("subagent result")))
	}))
	defer srv.Close()

	store, err := session.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	sess, _ := store.CreateSession(ctx)

	broker := approval.NewBroker(config.ApprovalConfig{AutoApproveAll: true}, nil)
	events := &mockSubagentEvents{}

	tool := agent.NewSpawnSubagentTool(
		llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"}),
		tools.NewRegistry(),
		store,
		broker,
		events,
		"",
		sess.ID,
	)

	params, _ := json.Marshal(map[string]string{"task": "do something"})
	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "subagent result" {
		t.Errorf("final text: got %q, want %q", result, "subagent result")
	}
	if len(events.starts) != 1 {
		t.Errorf("OnStart called %d time(s), want 1", len(events.starts))
	}
	if len(events.dones) != 1 {
		t.Errorf("OnDone called %d time(s), want 1", len(events.dones))
	}
	if len(events.errors) != 0 {
		t.Errorf("OnError called unexpectedly: %v", events.errors)
	}
}
