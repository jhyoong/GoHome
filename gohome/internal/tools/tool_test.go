package tools

import (
	"context"
	"encoding/json"
	"testing"
)

// fakeTool is a minimal Tool implementation for testing.
type fakeTool struct {
	name        string
	description string
	schema      json.RawMessage
}

func (f *fakeTool) Name() string             { return f.name }
func (f *fakeTool) Description() string      { return f.description }
func (f *fakeTool) InputSchema() json.RawMessage { return f.schema }
func (f *fakeTool) Execute(_ context.Context, _ json.RawMessage, _ ProgressSink) (Result, error) {
	return Result{Content: "ok"}, nil
}

func TestToolInterface(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{}}`)
	ft := &fakeTool{name: "fake", description: "a fake tool", schema: schema}

	var _ Tool = ft // compile-time check

	res, err := ft.Execute(context.Background(), nil, NullSink{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Content != "ok" {
		t.Errorf("expected 'ok', got %q", res.Content)
	}
}

func TestNullSink(t *testing.T) {
	// NullSink.Update must not panic and must be a no-op.
	var s NullSink
	s.Update("anything")
}
