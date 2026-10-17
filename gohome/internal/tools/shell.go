package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/config"
)

// shellWaitDelay bounds how long Wait blocks on output pipes after the
// process is killed or exits.
const shellWaitDelay = 2 * time.Second

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

// SudoPreauthFailedMarker is printed when sudo -v fails before the command
// runs. Sudo's own error (for example "Sorry, try again.") is printed above
// it, which the TUI uses to detect a rejected password.
const SudoPreauthFailedMarker = "gohome: sudo pre-authentication failed, command not run"

// wrapSudoPreauth returns a script that validates the sudo password read
// from fd 3, closes fd 3, then runs command unchanged. With no terminal,
// sudo caches credentials per parent process, so later sudo calls from
// this shell (including "sudo -n") succeed without a password. The final
// "exit $?" keeps the shell from exec-ing the last command in place of
// itself, which would change sudo's parent process.
func wrapSudoPreauth(command string) string {
	return "sudo -S -v -p '' <&3 || { echo '" + SudoPreauthFailedMarker + "' >&2; exit 1; }\n" +
		"exec 3<&-\n" +
		command + "\n" +
		"exit $?\n"
}

// sudoPasswordPipe returns the read end of a pipe that holds password
// followed by a newline. The caller must close it.
func sudoPasswordPipe(password string) (*os.File, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	_, err = w.WriteString(password + "\n")
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = r.Close()
		return nil, err
	}
	return r, nil
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

	script := inp.Command
	var passwordFile *os.File
	if pw := SudoPasswordFrom(ctx); pw != "" && runtime.GOOS != "windows" {
		f, err := sudoPasswordPipe(pw)
		if err != nil {
			return Result{IsError: true, Content: "shell: sudo password pipe: " + err.Error()}, nil
		}
		defer func() { _ = f.Close() }()
		passwordFile = f
		script = wrapSudoPreauth(inp.Command)
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", script)
	}
	detachFromTerminal(cmd)
	// If a grandchild keeps the output pipe open after the shell exits or is
	// killed, Wait returns after this delay instead of blocking.
	cmd.WaitDelay = shellWaitDelay

	if inp.CWD != nil {
		cmd.Dir = *inp.CWD
	}

	// The password reaches the child as fd 3, never as stdin.
	if passwordFile != nil {
		cmd.ExtraFiles = []*os.File{passwordFile}
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
			fmt.Fprintf(&captureBuf, "\n[shell: output cut off, truncated: %v]\n", err)
			// Keep draining so the command never blocks on a full pipe.
			_, _ = io.Copy(io.Discard, pr)
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

	// Distinguish timeout/cancellation from other errors. Output captured so
	// far is kept.
	if ctx.Err() == context.DeadlineExceeded {
		return Result{IsError: true, Content: withPartialOutput(fmt.Sprintf("shell: timed out after %dms", timeoutMs), captureBuf.String())}, nil
	}
	if ctx.Err() == context.Canceled {
		return Result{IsError: true, Content: withPartialOutput("shell: cancelled", captureBuf.String())}, nil
	}

	// Determine exit code.
	exitCode := 0
	bgNote := false
	if errors.Is(waitErr, exec.ErrWaitDelay) {
		// The shell finished but a background process still holds the
		// output pipe. Keep the output and the shell's own exit code.
		bgNote = true
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
	} else if waitErr != nil {
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
	if bgNote {
		content += "[shell: a background process still holds the output open; remaining output ignored]\n"
	}

	return Result{Content: content}, nil
}

// withPartialOutput appends captured output, if any, to a status message.
func withPartialOutput(msg, out string) string {
	out = strings.TrimRight(out, "\n")
	if out == "" {
		return msg
	}
	return msg + "\n" + out
}
