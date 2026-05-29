package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func execWrite(t *testing.T, input any) Result {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	wt := &WriteTool{}
	res, err := wt.Execute(context.Background(), raw, NullSink{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return res
}

func TestWrite_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "newfile.txt")

	res := execWrite(t, map[string]any{"path": path, "content": "hello world"})
	if res.IsError {
		t.Fatalf("unexpected IsError: %s", res.Content)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read written file: %v", err)
	}
	if string(got) != "hello world" {
		t.Errorf("got %q, want %q", string(got), "hello world")
	}
}

func TestWrite_OverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	os.WriteFile(path, []byte("old content"), 0644)

	res := execWrite(t, map[string]any{"path": path, "content": "new content"})
	if res.IsError {
		t.Fatalf("unexpected IsError: %s", res.Content)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read file: %v", err)
	}
	if string(got) != "new content" {
		t.Errorf("got %q, want %q", string(got), "new content")
	}
}

func TestWrite_MissingParentDir(t *testing.T) {
	path := "/nonexistent/deep/path/file.txt"
	res := execWrite(t, map[string]any{"path": path, "content": "anything"})
	if !res.IsError {
		t.Fatal("expected IsError when parent directory does not exist")
	}
	if res.Content == "" {
		t.Error("expected non-empty error message")
	}
}

func TestWrite_ToolMeta(t *testing.T) {
	wt := &WriteTool{}
	if wt.Name() != "write" {
		t.Errorf("expected name 'write', got %q", wt.Name())
	}
	if wt.Description() == "" {
		t.Error("expected non-empty description")
	}
	var schema map[string]any
	if err := json.Unmarshal(wt.InputSchema(), &schema); err != nil {
		t.Errorf("InputSchema is not valid JSON: %v", err)
	}
}
