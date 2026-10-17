package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// EditTool implements the "edit" tool.
type EditTool struct{}

func (EditTool) Name() string { return "edit" }

func (EditTool) Description() string {
	return "Perform an exact string replacement in a file. " +
		"old_string must match exactly once unless replace_all is true. " +
		"Returns an error if there is no match or if old_string is not unique."
}

var editSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "path":        {"type": "string",  "description": "Path of the file to edit"},
    "old_string":  {"type": "string",  "description": "Exact text to replace"},
    "new_string":  {"type": "string",  "description": "Replacement text"},
    "replace_all": {"type": "boolean", "description": "Replace every occurrence (default false)"}
  },
  "required": ["path", "old_string", "new_string"]
}`)

func (EditTool) InputSchema() json.RawMessage { return editSchema }

type editInput struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll *bool  `json:"replace_all"`
}

func (et EditTool) Execute(ctx context.Context, in json.RawMessage, _ ProgressSink) (Result, error) {
	var inp editInput
	if err := json.Unmarshal(in, &inp); err != nil {
		return Result{IsError: true, Content: "edit: invalid input: " + err.Error()}, nil
	}

	if inp.OldString == "" {
		return Result{IsError: true, Content: "edit: old_string must not be empty; use the write tool to create or overwrite a file"}, nil
	}

	// Read-before-edit check: if a session is present, the file must have been read first.
	if s := SessionFrom(ctx); s != nil {
		if !s.HasRead(inp.Path) {
			return Result{IsError: true, Content: "edit: file must be read first"}, nil
		}
	}

	data, err := os.ReadFile(inp.Path)
	if err != nil {
		return Result{IsError: true, Content: fmt.Sprintf("edit: cannot read %q: %v", inp.Path, err)}, nil
	}

	content := string(data)
	oldStr, newStr := inp.OldString, inp.NewString
	count := strings.Count(content, oldStr)

	// The read tool strips "\r", so the model sends LF-only text for CRLF
	// files. Retry with CRLF line endings when the plain match fails.
	if count == 0 && strings.Contains(content, "\r\n") &&
		strings.Contains(oldStr, "\n") && !strings.Contains(oldStr, "\r\n") {
		oldStr = strings.ReplaceAll(oldStr, "\n", "\r\n")
		newStr = strings.ReplaceAll(newStr, "\n", "\r\n")
		count = strings.Count(content, oldStr)
	}

	if count == 0 {
		return Result{IsError: true, Content: fmt.Sprintf("edit: no match for old_string in %q", inp.Path)}, nil
	}

	replaceAll := inp.ReplaceAll != nil && *inp.ReplaceAll
	if count > 1 && !replaceAll {
		return Result{
			IsError: true,
			Content: fmt.Sprintf("edit: old_string is not unique in %q (%d occurrences); use replace_all to replace all", inp.Path, count),
		}, nil
	}

	var updated string
	if replaceAll {
		updated = strings.ReplaceAll(content, oldStr, newStr)
	} else {
		updated = strings.Replace(content, oldStr, newStr, 1)
	}

	if err := os.WriteFile(inp.Path, []byte(updated), 0644); err != nil {
		return Result{IsError: true, Content: fmt.Sprintf("edit: cannot write %q: %v", inp.Path, err)}, nil
	}

	return Result{Content: fmt.Sprintf("edit: replaced %d occurrence(s) in %q", count, inp.Path)}, nil
}
