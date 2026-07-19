package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/config"
)

// ShellTool implements the "shell" tool.
type ShellTool struct {
	DefaultTimeoutMs int
	MaxTimeoutMs     int
}

func (s ShellTool) Name() string { return "shell" }

func (s ShellTool) Description() string {
	if runtime.GOOS == "windows" {
		return "Execute a PowerShell command. " +
			"Default timeout is 120 000 ms; max is 600 000 ms. " +
			"stdout and stderr are merged. " +
			"Non-zero exit codes are reported as normal results, not errors. " +
			"Timeout or kill failures return IsError."
	}
	return "Execute a shell command. " +
		"Default timeout is 120 000 ms; max is 600 000 ms. " +
		"stdout and stderr are merged. " +
		"Non-zero exit codes are reported as normal results, not errors. " +
		"Timeout or kill failures return IsError."
}

var shellSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "command":    {"type": "string",  "description": "Shell command to run"},
    "timeout_ms": {"type": "integer", "description": "Timeout in milliseconds (default 120000, max 600000)"},
    "cwd":        {"type": "string",  "description": "Working directory for the command"}
  },
  "required": ["command"]
}`)

func (s ShellTool) InputSchema() json.RawMessage { return shellSchema }

type shellInput struct {
	Command   string  `json:"command"`
	TimeoutMs *int    `json:"timeout_ms"`
	CWD       *string `json:"cwd"`
}

// sudoWordRe matches "sudo " followed by its first argument at command
// boundaries (start of string, or after ;, &, or | separators). The
// non-whitespace capture after "sudo " lets us check whether -S is already
// present.
var sudoWordRe = regexp.MustCompile(`(^|[;&|]\s*)sudo\s+(\S*)`)

// injectSudoS rewrites "sudo" to "sudo -S" at command boundaries so that
// sudo reads the password from stdin. If -S is already present, the command
// is returned unchanged.
func injectSudoS(command string) string {
	return sudoWordRe.ReplaceAllStringFunc(command, func(match string) string {
		if strings.Contains(match, "-S") {
			return match
		}
		return strings.Replace(match, "sudo ", "sudo -S ", 1)
	})
}

func (s ShellTool) Execute(ctx context.Context, in json.RawMessage, sink ProgressSink) (Result, error) {
	var inp shellInput
	if err := json.Unmarshal(in, &inp); err != nil {
		return Result{IsError: true, Content: "shell: invalid input: " + err.Error()}, nil
	}

	defaultTimeout := s.DefaultTimeoutMs
	if defaultTimeout <= 0 {
		defaultTimeout = config.DefaultShellTimeoutMs
	}
	maxTimeout := s.MaxTimeoutMs
	if maxTimeout <= 0 {
		maxTimeout = config.DefaultMaxShellTimeoutMs
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
		cmd = exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", inp.Command)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", inp.Command)
	}

	if inp.CWD != nil {
		cmd.Dir = *inp.CWD
	}

	// If a sudo password is stored in the context, pipe it to stdin and
	// ensure the command uses "sudo -S" so sudo reads from stdin.
	sudoPassword := SudoPasswordFrom(ctx)
	if sudoPassword != "" {
		cmd.Stdin = strings.NewReader(sudoPassword + "\n")
		if runtime.GOOS != "windows" {
			cmd.Args[len(cmd.Args)-1] = injectSudoS(inp.Command)
		}
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
			fmt.Fprintf(&captureBuf, "\n[shell: scanner error: %v]\n", err)
		}
	}()

	startErr := cmd.Start()
	if startErr != nil {
		_ = pw.Close()
		wg.Wait()
		// If the context was already cancelled/timed-out before start, report that.
		if ctx.Err() == context.DeadlineExceeded {
			return Result{IsError: true, Content: fmt.Sprintf("shell: timed out after %dms", timeoutMs)}, nil
		}
		if ctx.Err() == context.Canceled {
			return Result{IsError: true, Content: "shell: cancelled"}, nil
		}
		return Result{IsError: true, Content: fmt.Sprintf("shell: failed to start: %v", startErr)}, nil
	}

	waitErr := cmd.Wait()
	pw.CloseWithError(io.EOF)
	wg.Wait()

	// Distinguish timeout/cancellation from other errors.
	if ctx.Err() == context.DeadlineExceeded {
		return Result{IsError: true, Content: fmt.Sprintf("shell: timed out after %dms", timeoutMs)}, nil
	}
	if ctx.Err() == context.Canceled {
		return Result{IsError: true, Content: "shell: cancelled"}, nil
	}

	// Determine exit code.
	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// Non-exit error (e.g., process killed for other reasons).
			return Result{IsError: true, Content: fmt.Sprintf("shell: command error: %v", waitErr)}, nil
		}
	}

	captured := strings.TrimRight(captureBuf.String(), "\n")
	content := fmt.Sprintf("exit %d\n%s", exitCode, captured)
	if captured != "" {
		content += "\n"
	}

	return Result{Content: content}, nil
}
