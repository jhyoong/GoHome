package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

func mustWriteTemp(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "edit_test_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(content)
	f.Close()
	return f.Name()
}

func execEdit(t *testing.T, input any) Result {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	et := &EditTool{}
	res, err := et.Execute(context.Background(), raw, NullSink{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return res
}

func TestEdit_UniqueReplacement(t *testing.T) {
	path := mustWriteTemp(t, "hello world\n")

	res := execEdit(t, map[string]any{
		"path":       path,
		"old_string": "hello",
		"new_string": "goodbye",
	})
	if res.IsError {
		t.Fatalf("unexpected IsError: %s", res.Content)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "goodbye world\n" {
		t.Errorf("got %q, want %q", string(got), "goodbye world\n")
	}
}

func TestEdit_ReplaceAll(t *testing.T) {
	path := mustWriteTemp(t, "aaa bbb aaa ccc aaa\n")

	res := execEdit(t, map[string]any{
		"path":        path,
		"old_string":  "aaa",
		"new_string":  "zzz",
		"replace_all": true,
	})
	if res.IsError {
		t.Fatalf("unexpected IsError: %s", res.Content)
	}

	got, _ := os.ReadFile(path)
	if strings.Contains(string(got), "aaa") {
		t.Errorf("expected no 'aaa' remaining, got %q", string(got))
	}
	if strings.Count(string(got), "zzz") != 3 {
		t.Errorf("expected 3 'zzz' occurrences, got %q", string(got))
	}
}

func TestEdit_ErrorOnNonUniqueWithoutReplaceAll(t *testing.T) {
	path := mustWriteTemp(t, "aaa bbb aaa\n")

	res := execEdit(t, map[string]any{
		"path":       path,
		"old_string": "aaa",
		"new_string": "zzz",
	})
	if !res.IsError {
		t.Fatal("expected IsError when old_string is not unique")
	}
	if !strings.Contains(res.Content, "not unique") {
		t.Errorf("expected 'not unique' in error, got %q", res.Content)
	}
}

func TestEdit_ErrorOnNoMatch(t *testing.T) {
	path := mustWriteTemp(t, "hello world\n")

	res := execEdit(t, map[string]any{
		"path":       path,
		"old_string": "notpresent",
		"new_string": "replacement",
	})
	if !res.IsError {
		t.Fatal("expected IsError when old_string not found")
	}
	if !strings.Contains(res.Content, "no match") {
		t.Errorf("expected 'no match' in error, got %q", res.Content)
	}
}

func TestEdit_ToolMeta(t *testing.T) {
	et := &EditTool{}
	if et.Name() != "edit" {
		t.Errorf("expected name 'edit', got %q", et.Name())
	}
	if et.Description() == "" {
		t.Error("expected non-empty description")
	}
	var schema map[string]any
	if err := json.Unmarshal(et.InputSchema(), &schema); err != nil {
		t.Errorf("InputSchema is not valid JSON: %v", err)
	}
}

func TestEdit_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.txt")
	res := execEdit(t, map[string]any{
		"path":       path,
		"old_string": "x",
		"new_string": "y",
	})
	if !res.IsError {
		t.Fatal("expected IsError for missing file")
	}
}
