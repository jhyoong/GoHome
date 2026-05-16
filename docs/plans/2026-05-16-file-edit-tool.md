# file_edit Tool Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `file_edit` tool to `internal/tools/` that supports targeted edits (replace_text, replace_lines, apply_patch) without full file rewrites.

**Architecture:** One `FileEditTool` struct implementing the `Tool` interface. `Execute()` dispatches to private methods via a switch on `operation`. Pure Go implementation — no external processes, no cgo.

**Tech Stack:** Go stdlib only (`os`, `strings`, `fmt`, `encoding/json`)

---

### Task 1: Scaffold file_edit.go

**Files:**
- Create: `internal/tools/file_edit.go`

**Step 1: Create the skeleton**

Create `internal/tools/file_edit.go` with this content:

```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

type FileEditTool struct{}

func (f *FileEditTool) Name() string { return "file_edit" }
func (f *FileEditTool) Description() string {
	return "Edit a file in place using one of three operations: replace_text, replace_lines, or apply_patch."
}
func (f *FileEditTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path":       {"type": "string", "description": "Absolute or relative path to the file"},
			"operation":  {"type": "string", "enum": ["replace_text", "replace_lines", "apply_patch"]},
			"old_string": {"type": "string", "description": "Text to find (replace_text only)"},
			"new_string": {"type": "string", "description": "Replacement text (replace_text only)"},
			"start_line": {"type": "integer", "description": "First line to replace, 1-indexed (replace_lines only)"},
			"end_line":   {"type": "integer", "description": "Last line to replace, inclusive; defaults to start_line (replace_lines only)"},
			"content":    {"type": "string", "description": "Replacement content; empty string deletes the lines (replace_lines only)"},
			"patch":      {"type": "string", "description": "Unified diff patch to apply (apply_patch only)"}
		},
		"required": ["path", "operation"]
	}`)
}

func (f *FileEditTool) Execute(_ context.Context, params json.RawMessage) (string, error) {
	var p struct {
		Path      string `json:"path"`
		Operation string `json:"operation"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
		Content   string `json:"content"`
		Patch     string `json:"patch"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return "", fmt.Errorf("invalid params: %w", err)
	}
	switch p.Operation {
	case "replace_text":
		return f.replaceText(p.Path, p.OldString, p.NewString)
	case "replace_lines":
		return f.replaceLines(p.Path, p.StartLine, p.EndLine, p.Content)
	case "apply_patch":
		return f.applyPatch(p.Path, p.Patch)
	default:
		return "", fmt.Errorf("unknown operation: %q", p.Operation)
	}
}

func (f *FileEditTool) replaceText(path, oldStr, newStr string) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (f *FileEditTool) replaceLines(path string, startLine, endLine int, content string) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (f *FileEditTool) applyPatch(path, patch string) (string, error) {
	return "", fmt.Errorf("not implemented")
}
```

**Step 2: Verify it compiles**

```bash
go build ./internal/tools/
```

Expected: no output, exit 0.

**Step 3: Commit**

```bash
git add internal/tools/file_edit.go
git commit -m "feat: scaffold FileEditTool with stub operations"
```

---

### Task 2: Implement and test replace_text

**Files:**
- Create: `internal/tools/file_edit_test.go`
- Modify: `internal/tools/file_edit.go` (replaceText method)

**Step 1: Write failing tests**

Create `internal/tools/file_edit_test.go`:

```go
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
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/tools/ -run TestFileEditTool_ReplaceText -v
```

Expected: FAIL — `replaces first occurrence` fails with "not implemented".

**Step 3: Implement replaceText**

Replace the stub `replaceText` method in `file_edit.go`. Add `"os"` and `"strings"` to imports:

```go
func (f *FileEditTool) replaceText(path, oldStr, newStr string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(data)
	idx := strings.Index(content, oldStr)
	if idx == -1 {
		return "", fmt.Errorf("old_string not found in %s", path)
	}
	content = content[:idx] + newStr + content[idx+len(oldStr):]
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}
	return fmt.Sprintf("replaced text in %s", path), nil
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/tools/ -run TestFileEditTool_ReplaceText -v
```

Expected: all PASS.

**Step 5: Commit**

```bash
git add internal/tools/file_edit.go internal/tools/file_edit_test.go
git commit -m "feat: implement replace_text operation for FileEditTool"
```

---

### Task 3: Implement and test replace_lines

**Files:**
- Modify: `internal/tools/file_edit_test.go` (add tests)
- Modify: `internal/tools/file_edit.go` (replaceLines method)

**Step 1: Add failing tests**

Append to `file_edit_test.go`:

```go
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

	t.Run("end_line defaults to start_line (single-line insert)", func(t *testing.T) {
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
}
```

**Step 2: Run to verify they fail**

```bash
go test ./internal/tools/ -run TestFileEditTool_ReplaceLines -v
```

Expected: FAIL with "not implemented".

**Step 3: Implement replaceLines**

Replace the stub `replaceLines` method. The `strings` import should already be present from Task 2:

```go
func (f *FileEditTool) replaceLines(path string, startLine, endLine int, content string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	total := len(lines)

	if endLine == 0 {
		endLine = startLine
	}
	if startLine < 1 || startLine > total {
		return "", fmt.Errorf("start_line %d out of range (file has %d lines)", startLine, total)
	}
	if endLine < startLine {
		return "", fmt.Errorf("end_line must be >= start_line")
	}
	if endLine > total {
		return "", fmt.Errorf("end_line %d out of range (file has %d lines)", endLine, total)
	}

	var result []string
	result = append(result, lines[:startLine-1]...)
	if content != "" {
		result = append(result, strings.Split(content, "\n")...)
	}
	result = append(result, lines[endLine:]...)

	if err := os.WriteFile(path, []byte(strings.Join(result, "\n")), 0644); err != nil {
		return "", err
	}
	return fmt.Sprintf("replaced lines %d-%d in %s", startLine, endLine, path), nil
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/tools/ -run TestFileEditTool_ReplaceLines -v
```

Expected: all PASS.

**Step 5: Commit**

```bash
git add internal/tools/file_edit.go internal/tools/file_edit_test.go
git commit -m "feat: implement replace_lines operation for FileEditTool"
```

---

### Task 4: Implement and test apply_patch

**Files:**
- Modify: `internal/tools/file_edit_test.go` (add tests)
- Modify: `internal/tools/file_edit.go` (applyPatch + helpers)

**Step 1: Add failing tests**

Append to `file_edit_test.go`:

```go
func TestFileEditTool_ApplyPatch(t *testing.T) {
	tool := &tools.FileEditTool{}

	t.Run("applies a valid patch", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.txt")
		os.WriteFile(path, []byte("line1\nline2\nline3\nline4\nline5"), 0644)

		patch := `--- a/file.txt
+++ b/file.txt
@@ -2,3 +2,3 @@
 line2
-line3
+replaced
 line4
`
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

		patch := `--- a/file.txt
+++ b/file.txt
@@ -1,2 +1,2 @@
 WRONGCONTEXT
-line2
+replaced
`
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
}
```

**Step 2: Run to verify they fail**

```bash
go test ./internal/tools/ -run TestFileEditTool_ApplyPatch -v
```

Expected: FAIL with "not implemented".

**Step 3: Implement applyPatch and helpers**

Replace the stub `applyPatch` method and add two helper functions at the bottom of `file_edit.go`:

```go
func (f *FileEditTool) applyPatch(path, patch string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")

	patchLines := strings.Split(patch, "\n")
	i := 0
	// Skip file header lines (--- and +++ and anything before first @@)
	for i < len(patchLines) && !strings.HasPrefix(patchLines[i], "@@") {
		i++
	}

	for i < len(patchLines) {
		if !strings.HasPrefix(patchLines[i], "@@") {
			i++
			continue
		}
		hunkHeader := patchLines[i]
		origStart, err := parseHunkOrigStart(hunkHeader)
		if err != nil {
			return "", fmt.Errorf("invalid hunk header %q: %w", hunkHeader, err)
		}
		i++

		var hunkLines []string
		for i < len(patchLines) && !strings.HasPrefix(patchLines[i], "@@") {
			hunkLines = append(hunkLines, patchLines[i])
			i++
		}

		lines, err = applyHunk(lines, origStart, hunkLines, hunkHeader)
		if err != nil {
			return "", err
		}
	}

	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return "", err
	}
	return fmt.Sprintf("applied patch to %s", path), nil
}

// parseHunkOrigStart parses the original-file start line from a unified diff hunk header.
// Format: @@ -start[,count] +start[,count] @@
func parseHunkOrigStart(header string) (int, error) {
	var origStart int
	// Handle both "@@ -N,M +N,M @@" and "@@ -N +N @@" (count defaults to 1)
	_, err := fmt.Sscanf(header, "@@ -%d,", &origStart)
	if err != nil {
		_, err = fmt.Sscanf(header, "@@ -%d ", &origStart)
	}
	if err != nil {
		return 0, err
	}
	return origStart, nil
}

// applyHunk applies a single diff hunk to lines. origStart is 1-indexed.
func applyHunk(lines []string, origStart int, hunkLines []string, hunkHeader string) ([]string, error) {
	pos := origStart - 1 // convert to 0-indexed
	fileIdx := pos

	var result []string
	result = append(result, lines[:pos]...)

	for _, hl := range hunkLines {
		if len(hl) == 0 {
			continue
		}
		switch hl[0] {
		case ' ': // context line: must match
			if fileIdx >= len(lines) || lines[fileIdx] != hl[1:] {
				return nil, fmt.Errorf("hunk %s failed to apply: context not found", hunkHeader)
			}
			result = append(result, lines[fileIdx])
			fileIdx++
		case '-': // remove: must match, skip
			if fileIdx >= len(lines) || lines[fileIdx] != hl[1:] {
				return nil, fmt.Errorf("hunk %s failed to apply: context not found", hunkHeader)
			}
			fileIdx++
		case '+': // add: insert into result
			result = append(result, hl[1:])
		}
	}
	result = append(result, lines[fileIdx:]...)
	return result, nil
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/tools/ -run TestFileEditTool_ApplyPatch -v
```

Expected: all PASS.

**Step 5: Run all file_edit tests**

```bash
go test ./internal/tools/ -run TestFileEditTool -v
```

Expected: all PASS.

**Step 6: Commit**

```bash
git add internal/tools/file_edit.go internal/tools/file_edit_test.go
git commit -m "feat: implement apply_patch operation for FileEditTool"
```

---

### Task 5: Run the full test suite

**Step 1: Run all tests**

```bash
go test ./...
```

Expected: all PASS, no failures.

**Step 2: Run go vet**

```bash
go vet ./...
```

Expected: no output, exit 0.

---

### Task 6: Register FileEditTool in cmd/agent/main.go

**Files:**
- Modify: `cmd/agent/main.go`

**Step 1: Add the registration line**

Open `cmd/agent/main.go`. Find the block that registers the existing tools:

```go
reg.Register(&tools.ShellTool{})
reg.Register(&tools.FileReadTool{})
reg.Register(&tools.FileWriteTool{})
```

Add one line after `FileWriteTool`:

```go
reg.Register(&tools.FileEditTool{})
```

**Step 2: Build to verify**

```bash
make build
```

Expected: `go build -o gohome ./cmd/agent` completes with no errors.

**Step 3: Commit**

```bash
git add cmd/agent/main.go
git commit -m "feat: register FileEditTool in agent tool registry"
```
