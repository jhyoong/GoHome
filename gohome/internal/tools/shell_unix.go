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
}
