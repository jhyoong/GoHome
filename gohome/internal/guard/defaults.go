package guard

// cmdPos is the regex prefix for "the next word is the command being run":
//  1. start of the command, or after one of ; & | ( newline (plus one space)
//  2. zero or more wrapper words (sudo env time nice nohup exec command), each
//     followed only by flags (-n, --foo, optionally a numeric value as in
//     "nice -n 5") or VAR=value assignments, never free words
//  3. an optional path prefix such as /bin/ or /usr/bin/
//
// So "sudo -n rm" and "env FOO=1 /bin/rm" count, "echo rm" and "sudo echo rm" do not.
const cmdPos = `(^|[;&|(\n] ?)` +
	`((sudo|env|time|nice|nohup|exec|command)( (-[^\s;&|]+( [0-9]+)?|[A-Za-z_][A-Za-z0-9_]*=[^\s;&|]*))* )*` +
	`([^\s;&|]*/)?`

// DefaultDenyPatterns is the built-in set of shell command patterns that are
// blocked when no user-defined denylist file exists. Plain strings are substring
// matches; entries prefixed with "regex:" are full regular expressions.
// Commands are matched after runs of spaces and tabs are squeezed to one space.
//
// This is a safety net, not a sandbox: it catches common destructive commands,
// not every way of writing them.
var DefaultDenyPatterns = []string{
	// rm with a recursive flag (-r, -R, -rf, -fr, --recursive, in any order)
	// aimed at a standalone /, /*, ~, ~/, $HOME or ${HOME}, bare or quoted.
	// Other arguments may sit between the parts; shell separators end the match.
	// rm must be in command position (start, after a separator, sudo or a path).
	"regex:" + cmdPos + `rm( [^\s;&|]+)*? (-[A-Za-z]*[rR][A-Za-z]*|--recursive)( [^\s;&|]+)*? ["']?(/\*?|~/?|\$HOME/?|\$\{HOME\}/?)["']?([\s;&|)]|$)`,
	// mkfs and mkfs.<type> only when run as a command.
	"regex:" + cmdPos + `mkfs(\.[A-Za-z0-9]+)?([\s;&|)]|$)`,
	":(){ :|:& };:",
	`regex:>\s*/dev/sd[a-z]`,
}
