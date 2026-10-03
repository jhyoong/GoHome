package guard

import "regexp"

var sudoRe = regexp.MustCompile(`(^|[;&|(\n]\s*)sudo(\s|$)`)

// IsSudoCommand reports whether the given shell command invokes sudo.
// It matches sudo at the start of the command, after shell operators
// (;, &, &&, ||, |), after an opening parenthesis (subshells and $(...)),
// or at the start of a later line, but not as a substring of another word.
func IsSudoCommand(command string) bool {
	return sudoRe.MatchString(command)
}
