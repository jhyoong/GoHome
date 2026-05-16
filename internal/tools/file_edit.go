package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type FileEditTool struct{}

func (f *FileEditTool) Name() string { return "file_edit" }
func (f *FileEditTool) Description() string {
	return "Edit a file in place using one of three operations: replace_text, replace_lines, or apply_patch."
}
func (f *FileEditTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Absolute or relative path to the file"},"operation":{"type":"string","enum":["replace_text","replace_lines","apply_patch"]},"old_string":{"type":"string","description":"Text to find (replace_text only)"},"new_string":{"type":"string","description":"Replacement text (replace_text only)"},"start_line":{"type":"integer","description":"First line to replace, 1-indexed (replace_lines only)"},"end_line":{"type":"integer","description":"Last line to replace, inclusive; defaults to start_line (replace_lines only)"},"content":{"type":"string","description":"Replacement content; empty string deletes the lines (replace_lines only)"},"patch":{"type":"string","description":"Unified diff patch to apply (apply_patch only)"}},"required":["path","operation"]}`)
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

func (f *FileEditTool) replaceLines(path string, startLine, endLine int, content string) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (f *FileEditTool) applyPatch(path, patch string) (string, error) {
	return "", fmt.Errorf("not implemented")
}
