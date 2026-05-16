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
	if oldStr == "" {
		return "", fmt.Errorf("old_string must not be empty")
	}
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
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	raw := string(data)
	hasTrailingNewline := strings.HasSuffix(raw, "\n")
	if hasTrailingNewline {
		raw = raw[:len(raw)-1]
	}
	lines := strings.Split(raw, "\n")
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

	joined := strings.Join(result, "\n")
	if hasTrailingNewline {
		joined += "\n"
	}
	if err := os.WriteFile(path, []byte(joined), 0644); err != nil {
		return "", err
	}
	return fmt.Sprintf("replaced lines %d-%d in %s", startLine, endLine, path), nil
}

func (f *FileEditTool) applyPatch(path, patch string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")

	patchLines := strings.Split(patch, "\n")
	i := 0
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

func parseHunkOrigStart(header string) (int, error) {
	var origStart int
	_, err := fmt.Sscanf(header, "@@ -%d,", &origStart)
	if err != nil {
		_, err = fmt.Sscanf(header, "@@ -%d ", &origStart)
	}
	if err != nil {
		return 0, err
	}
	return origStart, nil
}

func applyHunk(lines []string, origStart int, hunkLines []string, hunkHeader string) ([]string, error) {
	pos := origStart - 1
	fileIdx := pos

	var result []string
	result = append(result, lines[:pos]...)

	for _, hl := range hunkLines {
		if len(hl) == 0 {
			continue
		}
		switch hl[0] {
		case ' ':
			if fileIdx >= len(lines) || lines[fileIdx] != hl[1:] {
				return nil, fmt.Errorf("hunk %s failed to apply: context not found", hunkHeader)
			}
			result = append(result, lines[fileIdx])
			fileIdx++
		case '-':
			if fileIdx >= len(lines) || lines[fileIdx] != hl[1:] {
				return nil, fmt.Errorf("hunk %s failed to apply: context not found", hunkHeader)
			}
			fileIdx++
		case '+':
			result = append(result, hl[1:])
		}
	}
	result = append(result, lines[fileIdx:]...)
	return result, nil
}
