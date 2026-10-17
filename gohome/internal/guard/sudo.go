package guard

import (
	"regexp"
	"runtime"
)

// sudoRe matches sudo in command position: after the start of the command,
// a shell operator (;, &, |), an opening ( { or backtick, or a newline,
// optionally followed by shell keywords and prefixes that start a command
// (do, then, else, elif, if, while, until, !, time, command, exec, nice,
// nohup, VAR=value, env VAR=value). The sudo word itself may be written as
// a path (/usr/bin/sudo) or escaped (\sudo).
var sudoRe = regexp.MustCompile(
	`(?:^|[;&|({` + "`" + `\n])` +
		`(?:\s*(?:do|then|else|elif|if|while|until|!|time|command|exec|nice|nohup|[A-Za-z_][A-Za-z0-9_]*=\S*|env(?:\s+[A-Za-z_][A-Za-z0-9_]*=\S*)*)\s)*` +
		`\s*(?:\\|[^\s;&|(){}` + "`" + `]*/)?sudo(?:\s|$)`)

// IsSudoCommand reports whether the given shell command invokes sudo.
// It matches sudo in command position (see sudoRe) but not as a substring
// of another word or as an argument to another command.
func IsSudoCommand(command string) bool {
	return sudoRe.MatchString(command)
}

// isSudoCommandOn is IsSudoCommand for the given GOOS. The shell tool does
// not feed the sudo password to PowerShell, so nothing counts as sudo on
// Windows and no password is asked for.
func isSudoCommandOn(goos, command string) bool {
	return goos != "windows" && IsSudoCommand(command)
}

// hostGOOS is the OS the guard decides for; tests override it.
var hostGOOS = runtime.GOOS

// needsSudoPassword reports whether command needs the sudo password prompt
// on the current OS.
func needsSudoPassword(command string) bool {
	return isSudoCommandOn(hostGOOS, command)
}
