//go:build windows

package tools

import "os/exec"

// detachFromTerminal is a no-op on Windows: there is no /dev/tty for child
// processes to grab, and PowerShell runs with -NonInteractive.
func detachFromTerminal(cmd *exec.Cmd) {}
