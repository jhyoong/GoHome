package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jhyoong/gohome/internal/tools"
)

func TestFileEditTool_ReplaceText(t *testing.T) {
	tool := &tools.FileEditTool{}

	t.Run("replaces first occurrence", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		os.WriteFile(path, []byte("hello world\nhello again"), 0644)

		params, _ := json.Marshal(map[string]any{
			"path":       path,
			"operation":  "replace_text",
			"old_string": "hello",
			"new_string": "bye",
		})
		_, err := tool.Execute(context.Background(), params)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != "bye world\nhello again" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("old_string not found", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		os.WriteFile(path, []byte("hello world"), 0644)

		params, _ := json.Marshal(map[string]any{
			"path":       path,
			"operation":  "replace_text",
			"old_string": "missing",
			"new_string": "x",
		})
		_, err := tool.Execute(context.Background(), params)
		if err == nil {
			t.Fatal("expected error for missing old_string")
		}
	})

	t.Run("unknown operation", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		os.WriteFile(path, []byte("content"), 0644)

		params, _ := json.Marshal(map[string]any{
			"path":      path,
			"operation": "bad_op",
		})
		_, err := tool.Execute(context.Background(), params)
		if err == nil {
			t.Fatal("expected error for unknown operation")
		}
	})
}
