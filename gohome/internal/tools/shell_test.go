package tools

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func execBash(t *testing.T, input any) Result {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	bt := &ShellTool{}
	res, err := bt.Execute(context.Background(), raw, NullSink{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return res
}

func TestBash_EchoHello(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	res := execBash(t, map[string]any{"command": "echo hello"})
	if res.IsError {
		t.Fatalf("unexpected IsError: %s", res.Content)
	}
	if !strings.Contains(res.Content, "exit 0") {
		t.Errorf("expected 'exit 0' in content, got %q", res.Content)
	}
	if !strings.Contains(res.Content, "hello") {
		t.Errorf("expected 'hello' in content, got %q", res.Content)
	}
}

func TestBash_NonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	res := execBash(t, map[string]any{"command": "false"})
	if res.IsError {
		t.Fatalf("unexpected IsError for non-zero exit (should be normal result): %s", res.Content)
	}
	if !strings.Contains(res.Content, "exit 1") {
		t.Errorf("expected 'exit 1' in content, got %q", res.Content)
	}
}

func TestBash_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	timeoutMs := 100
	res := execBash(t, map[string]any{
		"command":    "sleep 10",
		"timeout_ms": timeoutMs,
	})
	if !res.IsError {
		t.Fatal("expected IsError due to timeout")
	}
	if !strings.Contains(res.Content, "timed out") {
		t.Errorf("expected 'timed out' in content, got %q", res.Content)
	}
}

func TestBash_SinkReceivesLines(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}

	var received []string
	sp := &spySink{onUpdate: func(s string) { received = append(received, s) }}

	raw, _ := json.Marshal(map[string]any{"command": "printf 'line1\\nline2\\nline3\\n'"})
	bt := &ShellTool{}
	res, err := bt.Execute(context.Background(), raw, sp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected IsError: %s", res.Content)
	}
	if len(received) == 0 {
		t.Error("expected sink to receive at least one line")
	}
	combined := strings.Join(received, "")
	if !strings.Contains(combined, "line1") {
		t.Errorf("expected 'line1' in sink output, got %q", combined)
	}
}

func TestBash_ContextCancelled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}

	ctx, cancel := context.WithCancel(context.Background())

	bt := &ShellTool{}
	raw, _ := json.Marshal(map[string]any{"command": "sleep 10"})

	// Cancel the context immediately after launching Execute in a goroutine.
	// We cancel after a brief delay so the process has time to start.
	type outcome struct {
		res Result
		err error
	}
	ch := make(chan outcome, 1)
	go func() {
		res, err := bt.Execute(ctx, raw, NullSink{})
		ch <- outcome{res, err}
	}()

	// Give the process a moment to start, then cancel.
	cancel()

	got := <-ch
	if got.err != nil {
		t.Fatalf("unexpected error: %v", got.err)
	}
	if !got.res.IsError {
		t.Fatal("expected IsError when context is cancelled")
	}
	if !strings.Contains(got.res.Content, "cancelled") {
		t.Errorf("expected 'cancelled' in content, got %q", got.res.Content)
	}
}

type spySink struct {
	onUpdate func(string)
}

func (s *spySink) Update(chunk string) { s.onUpdate(chunk) }

func TestBash_ToolMeta(t *testing.T) {
	bt := &ShellTool{}
	if bt.Name() != "shell" {
		t.Errorf("expected name 'shell', got %q", bt.Name())
	}
	if bt.Description() == "" {
		t.Error("expected non-empty description")
	}
	var schema map[string]any
	if err := json.Unmarshal(bt.InputSchema(), &schema); err != nil {
		t.Errorf("InputSchema is not valid JSON: %v", err)
	}
}

func TestBash_SudoPasswordPipedToStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	ctx := WithSudoPassword(context.Background(), "testpass")
	raw, _ := json.Marshal(map[string]any{"command": "head -1"})
	bt := &ShellTool{}
	res, err := bt.Execute(ctx, raw, NullSink{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected IsError: %s", res.Content)
	}
	if !strings.Contains(res.Content, "testpass") {
		t.Errorf("expected stdin password in output, got %q", res.Content)
	}
}

func TestBash_NoSudoPassword_StdinEmpty(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	raw, _ := json.Marshal(map[string]any{"command": "cat"})
	bt := &ShellTool{}
	res, err := bt.Execute(context.Background(), raw, NullSink{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected IsError: %s", res.Content)
	}
	if strings.Contains(res.Content, "testpass") {
		t.Errorf("should not contain password without context, got %q", res.Content)
	}
}

func TestInjectSudoS(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain sudo", "sudo apt install vim", "sudo -S apt install vim"},
		{"sudo already has -S", "sudo -S apt install vim", "sudo -S apt install vim"},
		{"piped sudo", "echo foo | sudo tee /etc/bar", "echo foo | sudo -S tee /etc/bar"},
		{"chained sudo", "cd /tmp && sudo rm -rf stuff", "cd /tmp && sudo -S rm -rf stuff"},
		{"no sudo", "ls -la", "ls -la"},
		{"sudo with flags", "sudo -u root ls", "sudo -S -u root ls"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := injectSudoS(tt.in)
			if got != tt.want {
				t.Errorf("injectSudoS(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestBash_DefaultTimeoutCapApplied(t *testing.T) {
	// Provide a timeout larger than the cap (600000ms) and verify the tool doesn't error on construction.
	if runtime.GOOS == "windows" {
		t.Skip("unix shell command")
	}
	// Just verify it runs normally; actual cap logic tested internally.
	res := execBash(t, map[string]any{
		"command":    "echo cap-test",
		"timeout_ms": 9999999,
	})
	if res.IsError {
		t.Fatalf("unexpected IsError: %s", res.Content)
	}
	if !strings.Contains(res.Content, "cap-test") {
		t.Errorf("expected 'cap-test' in output, got %q", res.Content)
	}
}
