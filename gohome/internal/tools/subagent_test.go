package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// mustJSON marshals v to JSON or fatally fails the test.
func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("mustJSON: %v", err)
	}
	return json.RawMessage(b)
}

// fakeSpawner records the last call and returns scripted values.
type fakeSpawner struct {
	lastTask         string
	lastSystemPrompt string
	resultText       string
	isError          bool
	err              error
}

func (f *fakeSpawner) Spawn(_ context.Context, task, systemPrompt string) (string, bool, error) {
	f.lastTask = task
	f.lastSystemPrompt = systemPrompt
	return f.resultText, f.isError, f.err
}

func TestSubagentTool_Name(t *testing.T) {
	tool := NewSubagentTool(&fakeSpawner{})
	if tool.Name() != "subagent" {
		t.Errorf("Name: got %q, want %q", tool.Name(), "subagent")
	}
}

func TestSubagentTool_ForwardsTaskAndSystemPrompt(t *testing.T) {
	spawner := &fakeSpawner{resultText: "child result", isError: false}
	tool := NewSubagentTool(spawner)

	in := mustJSON(t, map[string]any{
		"task":          "do something useful",
		"system_prompt": "you are a specialist",
	})

	res, err := tool.Execute(context.Background(), in, NullSink{})
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if res.IsError {
		t.Errorf("IsError: got true, want false; content: %s", res.Content)
	}
	if res.Content != "child result" {
		t.Errorf("Content: got %q, want %q", res.Content, "child result")
	}
	if spawner.lastTask != "do something useful" {
		t.Errorf("task forwarded: got %q, want %q", spawner.lastTask, "do something useful")
	}
	if spawner.lastSystemPrompt != "you are a specialist" {
		t.Errorf("system_prompt forwarded: got %q, want %q", spawner.lastSystemPrompt, "you are a specialist")
	}
}

func TestSubagentTool_EmptySystemPrompt(t *testing.T) {
	spawner := &fakeSpawner{resultText: "ok"}
	tool := NewSubagentTool(spawner)

	in := mustJSON(t, map[string]any{"task": "a task"})
	res, err := tool.Execute(context.Background(), in, NullSink{})
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if res.IsError {
		t.Errorf("IsError: got true, want false")
	}
	if spawner.lastSystemPrompt != "" {
		t.Errorf("system_prompt: got %q, want empty", spawner.lastSystemPrompt)
	}
}

func TestSubagentTool_SpawnerIsError(t *testing.T) {
	spawner := &fakeSpawner{resultText: "bad news", isError: true}
	tool := NewSubagentTool(spawner)

	in := mustJSON(t, map[string]any{"task": "fail task"})
	res, err := tool.Execute(context.Background(), in, NullSink{})
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if !res.IsError {
		t.Errorf("IsError: got false, want true")
	}
	if res.Content != "bad news" {
		t.Errorf("Content: got %q, want %q", res.Content, "bad news")
	}
}

func TestSubagentTool_SpawnerReturnsErr(t *testing.T) {
	spawner := &fakeSpawner{err: errors.New("spawn failed")}
	tool := NewSubagentTool(spawner)

	in := mustJSON(t, map[string]any{"task": "crash task"})
	res, err := tool.Execute(context.Background(), in, NullSink{})
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if !res.IsError {
		t.Errorf("IsError: got false, want true")
	}
}

func TestSubagentTool_InvalidJSON(t *testing.T) {
	tool := NewSubagentTool(&fakeSpawner{})
	res, err := tool.Execute(context.Background(), []byte(`not-json`), NullSink{})
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if !res.IsError {
		t.Errorf("IsError: got false, want true for invalid JSON")
	}
}
