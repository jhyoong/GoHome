package tools

import (
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

// timeouts returns the default and maximum timeouts in milliseconds,
// falling back to the config defaults when unset.
func (s ShellTool) timeouts() (defaultMs, maxMs int) {
	defaultMs = s.DefaultTimeoutMs
	if defaultMs <= 0 {
		defaultMs = config.DefaultShellTimeoutMs
	}
	maxMs = s.MaxTimeoutMs
	if maxMs <= 0 {
		maxMs = config.DefaultMaxShellTimeoutMs
	}
	return defaultMs, maxMs
}

func (s ShellTool) Description() string {
	defaultMs, maxMs := s.timeouts()
	kind := "shell"
	if runtime.GOOS == "windows" {
		kind = "PowerShell"
	}
	return fmt.Sprintf("Execute a %s command. "+
		"Default timeout is %d ms; max is %d ms. "+
		"stdout and stderr are merged. "+
		"Long output keeps the first and last %d KiB. "+
		"Non-zero exit codes are reported as normal results, not errors. "+
		"Timeout or kill failures return IsError.",
		kind, defaultMs, maxMs, shellOutputKeep/1024)
}

func (s ShellTool) InputSchema() json.RawMessage {
	defaultMs, maxMs := s.timeouts()
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{"type": "string", "description": "Shell command to run"},
			"timeout_ms": map[string]any{"type": "integer", "description": fmt.Sprintf(
				"Timeout in milliseconds (default %d, max %d)", defaultMs, maxMs)},
			"cwd": map[string]any{"type": "string", "description": "Working directory for the command"},
		},
		"required": []string{"command"},
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		panic("shell: marshal schema: " + err.Error())
	}
	return raw
}

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
// itself, which would change sudo's parent process. The blank line after
// command keeps a trailing backslash from joining the exit line.
func wrapSudoPreauth(command string) string {
	return "sudo -S -v -p '' <&3 || { echo '" + SudoPreauthFailedMarker + "' >&2; exit 1; }\n" +
		"exec 3<&-\n" +
		command + "\n\n" +
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

	defaultTimeout, maxTimeout := s.timeouts()

	// A missing, zero or negative timeout_ms means the default.
	timeoutMs := defaultTimeout
	if inp.TimeoutMs != nil && *inp.TimeoutMs > 0 {
		timeoutMs = min(*inp.TimeoutMs, maxTimeout)
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

	// Only the first and last shellOutputKeep bytes are kept. The pipe is
	// read in chunks until EOF, so the command never blocks on a full pipe
	// and lines of any length are fine. Chunks sent to the sink may split
	// a line or a multi-byte rune.
	captureBuf := newCappedBuffer(shellOutputKeep, shellOutputKeep)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := pr.Read(buf)
			if n > 0 {
				_, _ = captureBuf.Write(buf[:n])
				sink.Update(string(buf[:n]))
			}
			if err != nil {
				return
			}
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
