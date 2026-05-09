package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

type FileReadTool struct{}

func (f *FileReadTool) Name() string        { return "file_read" }
func (f *FileReadTool) Description() string { return "Read a file and return its contents as a string." }
func (f *FileReadTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Absolute or relative path to the file"}},"required":["path"]}`)
}

func (f *FileReadTool) Execute(_ context.Context, params json.RawMessage) (string, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return "", fmt.Errorf("invalid params: %w", err)
	}
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
