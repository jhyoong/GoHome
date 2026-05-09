package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jhyoong/gohome/internal/tools"
)

type mockTool struct{ name string }

func (m *mockTool) Name() string                                                    { return m.name }
func (m *mockTool) Description() string                                             { return "mock" }
func (m *mockTool) Parameters() json.RawMessage                                     { return json.RawMessage(`{}`) }
func (m *mockTool) Execute(_ context.Context, _ json.RawMessage) (string, error) { return "ok", nil }

func TestRegistry(t *testing.T) {
	reg := tools.NewRegistry()

	if err := reg.Register(&mockTool{"foo"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := reg.Register(&mockTool{"foo"}); err == nil {
		t.Error("expected error for duplicate")
	}

	tool, ok := reg.Get("foo")
	if !ok || tool.Name() != "foo" {
		t.Error("Get failed")
	}

	reg.Deregister("foo")
	if _, ok := reg.Get("foo"); ok {
		t.Error("tool still present after Deregister")
	}

	reg.Register(&mockTool{"a"})
	reg.Register(&mockTool{"b"})
	if len(reg.All()) != 2 {
		t.Errorf("want 2 tools, got %d", len(reg.All()))
	}
}

func TestToLLMTools(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&mockTool{"mytool"})
	llmTools := reg.ToLLMTools()
	if len(llmTools) != 1 {
		t.Fatalf("want 1 tool def, got %d", len(llmTools))
	}
	fn := llmTools[0]["function"].(map[string]any)
	if fn["name"] != "mytool" {
		t.Errorf("got name %q", fn["name"])
	}
}
