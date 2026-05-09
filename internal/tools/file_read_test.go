package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jhyoong/gohome/internal/tools"
)

func TestFileReadTool(t *testing.T) {
	f, _ := os.CreateTemp("", "test*.txt")
	f.WriteString("hello file")
	f.Close()
	defer os.Remove(f.Name())

	tool := &tools.FileReadTool{}
	params, _ := json.Marshal(map[string]string{"path": f.Name()})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "hello file" {
		t.Errorf("got %q", result)
	}
}
