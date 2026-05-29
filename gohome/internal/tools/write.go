package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WriteTool implements the "write" tool.
type WriteTool struct{}

func (WriteTool) Name() string { return "write" }

func (WriteTool) Description() string {
	return "Create or overwrite a file with the given content. " +
		"The parent directory must already exist; it will not be created automatically."
}

var writeSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "path":    {"type": "string", "description": "Path of the file to write"},
    "content": {"type": "string", "description": "Content to write to the file"}
  },
  "required": ["path", "content"]
}`)

func (WriteTool) InputSchema() json.RawMessage { return writeSchema }

type writeInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (WriteTool) Execute(_ context.Context, in json.RawMessage, _ ProgressSink) (Result, error) {
	var inp writeInput
	if err := json.Unmarshal(in, &inp); err != nil {
		return Result{IsError: true, Content: "write: invalid input: " + err.Error()}, nil
	}

	// Verify parent directory exists.
	parent := filepath.Dir(inp.Path)
	if _, err := os.Stat(parent); os.IsNotExist(err) {
		return Result{IsError: true, Content: fmt.Sprintf("write: parent directory %q does not exist", parent)}, nil
	}

	if err := os.WriteFile(inp.Path, []byte(inp.Content), 0644); err != nil {
		return Result{IsError: true, Content: fmt.Sprintf("write: cannot write %q: %v", inp.Path, err)}, nil
	}

	return Result{Content: fmt.Sprintf("write: wrote %d bytes to %q", len(inp.Content), inp.Path)}, nil
}
