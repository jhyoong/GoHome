package guard

import "regexp"

// sudoRe matches sudo in command position: after the start of the command,
// a shell operator (;, &, |), an opening ( { or backtick, or a newline,
// optionally followed by shell keywords and prefixes that start a command
// (do, then, else, elif, if, while, until, !, time, env VAR=value).
var sudoRe = regexp.MustCompile(
	`(?:^|[;&|({` + "`" + `\n])` +
		`(?:\s*(?:do|then|else|elif|if|while|until|!|time|env(?:\s+[A-Za-z_][A-Za-z0-9_]*=\S*)*)\s)*` +
		`\s*sudo(?:\s|$)`)

// IsSudoCommand reports whether the given shell command invokes sudo.
// It matches sudo in command position (see sudoRe) but not as a substring
// of another word or as an argument to another command.
func IsSudoCommand(command string) bool {
	return sudoRe.MatchString(command)
}
