package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type FileWriteTool struct{}

func (f *FileWriteTool) Name() string { return "file_write" }
func (f *FileWriteTool) Description() string {
	return "Write content to a file, creating parent directories as needed."
}
func (f *FileWriteTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
}

func (f *FileWriteTool) Execute(_ context.Context, params json.RawMessage) (string, error) {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return "", fmt.Errorf("invalid params: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(p.Path), 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(p.Path, []byte(p.Content), 0644); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(p.Content), p.Path), nil
}
