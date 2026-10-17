//go:build !windows

package tools

import (
	"os/exec"
	"syscall"
)

// detachFromTerminal starts the command in a new session with no controlling
// terminal, so programs that open /dev/tty (sudo, ssh password prompts) fail
// instead of drawing over the TUI and stealing keystrokes.
func detachFromTerminal(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// Setsid makes the shell a process group leader, so killing the negative
	// pid also kills its children and background jobs.
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
