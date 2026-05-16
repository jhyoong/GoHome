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

	t.Run("empty old_string is rejected", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		os.WriteFile(path, []byte("hello"), 0644)

		params, _ := json.Marshal(map[string]any{
			"path":       path,
			"operation":  "replace_text",
			"old_string": "",
			"new_string": "x",
		})
		_, err := tool.Execute(context.Background(), params)
		if err == nil {
			t.Fatal("expected error for empty old_string")
		}
	})

	t.Run("empty new_string deletes the match", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		os.WriteFile(path, []byte("hello world"), 0644)

		params, _ := json.Marshal(map[string]any{
			"path":       path,
			"operation":  "replace_text",
			"old_string": "hello ",
			"new_string": "",
		})
		_, err := tool.Execute(context.Background(), params)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != "world" {
			t.Errorf("got %q", got)
		}
	})
}

func TestFileEditTool_ReplaceLines(t *testing.T) {
	tool := &tools.FileEditTool{}

	t.Run("replaces middle lines", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		os.WriteFile(path, []byte("line1\nline2\nline3\nline4"), 0644)

		params, _ := json.Marshal(map[string]any{
			"path":       path,
			"operation":  "replace_lines",
			"start_line": 2,
			"end_line":   3,
			"content":    "replaced",
		})
		_, err := tool.Execute(context.Background(), params)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != "line1\nreplaced\nline4" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("end_line defaults to start_line (single-line replace)", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		os.WriteFile(path, []byte("line1\nline2\nline3"), 0644)

		params, _ := json.Marshal(map[string]any{
			"path":       path,
			"operation":  "replace_lines",
			"start_line": 2,
			"content":    "new2",
		})
		_, err := tool.Execute(context.Background(), params)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != "line1\nnew2\nline3" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("empty content deletes lines", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		os.WriteFile(path, []byte("line1\nline2\nline3"), 0644)

		params, _ := json.Marshal(map[string]any{
			"path":       path,
			"operation":  "replace_lines",
			"start_line": 2,
			"end_line":   2,
			"content":    "",
		})
		_, err := tool.Execute(context.Background(), params)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != "line1\nline3" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("start_line out of range", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		os.WriteFile(path, []byte("line1\nline2"), 0644)

		params, _ := json.Marshal(map[string]any{
			"path":       path,
			"operation":  "replace_lines",
			"start_line": 99,
			"content":    "x",
		})
		_, err := tool.Execute(context.Background(), params)
		if err == nil {
			t.Fatal("expected error for out-of-range start_line")
		}
	})

	t.Run("end_line less than start_line", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		os.WriteFile(path, []byte("line1\nline2\nline3"), 0644)

		params, _ := json.Marshal(map[string]any{
			"path":       path,
			"operation":  "replace_lines",
			"start_line": 3,
			"end_line":   1,
			"content":    "x",
		})
		_, err := tool.Execute(context.Background(), params)
		if err == nil {
			t.Fatal("expected error when end_line < start_line")
		}
	})

	t.Run("replaces first line", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		os.WriteFile(path, []byte("line1\nline2\nline3"), 0644)

		params, _ := json.Marshal(map[string]any{
			"path":       path,
			"operation":  "replace_lines",
			"start_line": 1,
			"end_line":   1,
			"content":    "new1",
		})
		_, err := tool.Execute(context.Background(), params)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != "new1\nline2\nline3" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("replaces last line", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		os.WriteFile(path, []byte("line1\nline2\nline3"), 0644)

		params, _ := json.Marshal(map[string]any{
			"path":       path,
			"operation":  "replace_lines",
			"start_line": 3,
			"end_line":   3,
			"content":    "new3",
		})
		_, err := tool.Execute(context.Background(), params)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != "line1\nline2\nnew3" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("preserves trailing newline", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0644)

		params, _ := json.Marshal(map[string]any{
			"path":       path,
			"operation":  "replace_lines",
			"start_line": 2,
			"end_line":   2,
			"content":    "replaced",
		})
		_, err := tool.Execute(context.Background(), params)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != "line1\nreplaced\nline3\n" {
			t.Errorf("got %q", got)
		}
	})
}

func TestFileEditTool_ApplyPatch(t *testing.T) {
	tool := &tools.FileEditTool{}

	t.Run("applies a valid patch", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		os.WriteFile(path, []byte("line1\nline2\nline3\nline4\nline5"), 0644)

		patch := "--- a/file.txt\n+++ b/file.txt\n@@ -2,3 +2,3 @@\n line2\n-line3\n+replaced\n line4\n"
		params, _ := json.Marshal(map[string]any{
			"path":      path,
			"operation": "apply_patch",
			"patch":     patch,
		})
		_, err := tool.Execute(context.Background(), params)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != "line1\nline2\nreplaced\nline4\nline5" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("patch with context mismatch fails", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		os.WriteFile(path, []byte("line1\nline2\nline3"), 0644)

		patch := "--- a/file.txt\n+++ b/file.txt\n@@ -1,2 +1,2 @@\n WRONGCONTEXT\n-line2\n+replaced\n"
		params, _ := json.Marshal(map[string]any{
			"path":      path,
			"operation": "apply_patch",
			"patch":     patch,
		})
		_, err := tool.Execute(context.Background(), params)
		if err == nil {
			t.Fatal("expected error for context mismatch")
		}
	})

	t.Run("applies multi-hunk patch with line delta", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		os.WriteFile(path, []byte("line1\nline2\nline3\nline4\nline5\nline6"), 0644)

		// Hunk 1: replace line2 with two lines (adds 1 line, delta=+1)
		// Hunk 2: delete line5 (which is now line6 in modified file, but origStart=5 + offset=1 = 6)
		patch := "--- a/file.txt\n+++ b/file.txt\n@@ -2,1 +2,2 @@\n-line2\n+line2a\n+line2b\n@@ -5,1 +6,0 @@\n-line5\n"
		params, _ := json.Marshal(map[string]any{
			"path":      path,
			"operation": "apply_patch",
			"patch":     patch,
		})
		_, err := tool.Execute(context.Background(), params)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		got, _ := os.ReadFile(path)
		want := "line1\nline2a\nline2b\nline3\nline4\nline6"
		if string(got) != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}
