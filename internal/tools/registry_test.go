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

func TestRegistryCloneWith(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&tools.ShellTool{})

	extra := &tools.FileReadTool{}
	clone := reg.CloneWith(extra)

	// Original unchanged
	if _, ok := reg.Get("file_read"); ok {
		t.Error("original registry should not have file_read")
	}

	// Clone has both
	if _, ok := clone.Get("shell"); !ok {
		t.Error("clone missing shell tool")
	}
	if _, ok := clone.Get("file_read"); !ok {
		t.Error("clone missing file_read tool")
	}

	// nil entries in extra must not panic
	clone2 := reg.CloneWith(nil, &tools.FileReadTool{})
	if _, ok := clone2.Get("file_read"); !ok {
		t.Error("clone2 missing file_read after nil in extra")
	}

	// extra tool with same name as existing tool overwrites in clone, not in original
	// Use mockTool (non-zero size) to avoid Go's zero-size pointer aliasing.
	original := &mockTool{"dup"}
	reg.Register(original)
	overrider := &mockTool{"dup"}
	clone3 := reg.CloneWith(overrider)
	got, _ := clone3.Get("dup")
	if got != overrider {
		t.Error("extra tool did not overwrite existing tool with same name in clone")
	}
	origDup, _ := reg.Get("dup")
	if origDup == overrider {
		t.Error("original registry was mutated by CloneWith overwrite")
	}
}
