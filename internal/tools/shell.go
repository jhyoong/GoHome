package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
)

type ShellTool struct{}

func (s *ShellTool) Name() string { return "shell" }
func (s *ShellTool) Description() string {
	return "Execute a shell command. Returns stdout and stderr combined."
}
func (s *ShellTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"The shell command to execute"}},"required":["command"]}`)
}

func (s *ShellTool) Execute(ctx context.Context, params json.RawMessage) (string, error) {
	var p struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return "", fmt.Errorf("invalid params: %w", err)
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", p.Command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", p.Command)
	}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	_ = cmd.Run() // non-zero exit is not an error; output contains the result
	return buf.String(), nil
}
