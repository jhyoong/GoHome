package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/config"
)

// BashTool implements the "bash" tool.
type BashTool struct {
	DefaultTimeoutMs int
	MaxTimeoutMs     int
}

func (b BashTool) Name() string { return "bash" }

func (b BashTool) Description() string {
	return "Execute a shell command. " +
		"Default timeout is 120 000 ms; max is 600 000 ms. " +
		"stdout and stderr are merged. " +
		"Non-zero exit codes are reported as normal results, not errors. " +
		"Timeout or kill failures return IsError."
}

var bashSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "command":    {"type": "string",  "description": "Shell command to run"},
    "timeout_ms": {"type": "integer", "description": "Timeout in milliseconds (default 120000, max 600000)"},
    "cwd":        {"type": "string",  "description": "Working directory for the command"}
  },
  "required": ["command"]
}`)

func (b BashTool) InputSchema() json.RawMessage { return bashSchema }

type bashInput struct {
	Command   string  `json:"command"`
	TimeoutMs *int    `json:"timeout_ms"`
	CWD       *string `json:"cwd"`
}

func (b BashTool) Execute(ctx context.Context, in json.RawMessage, sink ProgressSink) (Result, error) {
	var inp bashInput
	if err := json.Unmarshal(in, &inp); err != nil {
		return Result{IsError: true, Content: "bash: invalid input: " + err.Error()}, nil
	}

	defaultTimeout := b.DefaultTimeoutMs
	if defaultTimeout <= 0 {
		defaultTimeout = config.DefaultBashTimeoutMs
	}
	maxTimeout := b.MaxTimeoutMs
	if maxTimeout <= 0 {
		maxTimeout = config.DefaultMaxBashTimeoutMs
	}

	timeoutMs := defaultTimeout
	if inp.TimeoutMs != nil {
		timeoutMs = *inp.TimeoutMs
		if timeoutMs > maxTimeout {
			timeoutMs = maxTimeout
		}
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", inp.Command)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", inp.Command)
	}

	if inp.CWD != nil {
		cmd.Dir = *inp.CWD
	}

	// Pipe stdout+stderr through a reader that fans out to: sink + capture buffer.
	pr, pw := io.Pipe()

	cmd.Stdout = pw
	cmd.Stderr = pw

	var (
		captureBuf bytes.Buffer
		wg         sync.WaitGroup
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(pr)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text() + "\n"
			captureBuf.WriteString(line)
			sink.Update(line)
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(&captureBuf, "\n[bash: scanner error: %v]\n", err)
		}
	}()

	startErr := cmd.Start()
	if startErr != nil {
		_ = pw.Close()
		wg.Wait()
		// If the context was already cancelled/timed-out before start, report that.
		if ctx.Err() == context.DeadlineExceeded {
			return Result{IsError: true, Content: fmt.Sprintf("bash: timed out after %dms", timeoutMs)}, nil
		}
		if ctx.Err() == context.Canceled {
			return Result{IsError: true, Content: "bash: cancelled"}, nil
		}
		return Result{IsError: true, Content: fmt.Sprintf("bash: failed to start: %v", startErr)}, nil
	}

	waitErr := cmd.Wait()
	pw.CloseWithError(io.EOF)
	wg.Wait()

	// Distinguish timeout/cancellation from other errors.
	if ctx.Err() == context.DeadlineExceeded {
		return Result{IsError: true, Content: fmt.Sprintf("bash: timed out after %dms", timeoutMs)}, nil
	}
	if ctx.Err() == context.Canceled {
		return Result{IsError: true, Content: "bash: cancelled"}, nil
	}

	// Determine exit code.
	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// Non-exit error (e.g., process killed for other reasons).
			return Result{IsError: true, Content: fmt.Sprintf("bash: command error: %v", waitErr)}, nil
		}
	}

	captured := strings.TrimRight(captureBuf.String(), "\n")
	content := fmt.Sprintf("exit %d\n%s", exitCode, captured)
	if captured != "" {
		content += "\n"
	}

	return Result{Content: content}, nil
}
