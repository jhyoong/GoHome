package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jhyoong/gohome/internal/tools"
)

func TestShellTool(t *testing.T) {
	tool := &tools.ShellTool{}
	params, _ := json.Marshal(map[string]string{"command": "echo hello"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "hello\n" {
		t.Errorf("got %q, want %q", result, "hello\n")
	}
}
