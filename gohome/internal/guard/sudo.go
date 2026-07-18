package guard

import "regexp"

var sudoRe = regexp.MustCompile(`(^|[;&|]\s*)sudo(\s|$)`)

// IsSudoCommand reports whether the given shell command invokes sudo.
// It matches sudo at the start of the command or after shell operators
// (;, &, &&, ||, |) but not as a substring of another word.
func IsSudoCommand(command string) bool {
	return sudoRe.MatchString(command)
}
