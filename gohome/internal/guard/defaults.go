package guard

// cmdPos is the regex prefix for "the next word is the command being run":
//  1. start of the command, or after one of ; & | ( { ! ` or newline (plus one
//     space), optionally followed by a shell keyword (then do else elif)
//  2. zero or more leading VAR=value assignments and wrapper words (sudo env
//     time nice nohup exec command xargs doas); each wrapper is followed only by
//     flags (-n, --foo), each flag optionally taking one non-dash value
//     ("sudo -u root", "nice -n 5"), or by VAR=value. A free word with no flag
//     before it ends the wrapper chain, so "sudo echo rm" is not a match
//  3. an optional path prefix such as /bin/ or /usr/bin/, then an optional
//     backslash (\rm)
//
// A missed "rm -rf /" costs far more than a rare false positive, so this errs
// on the side of matching.
const cmdPos = `(^|[;&|(\n` + "`" + `{!] ?)((then|do|else|elif) )?` +
	`(([A-Za-z_][A-Za-z0-9_]*=[^\s;&|]*|(sudo|env|time|nice|nohup|exec|command|xargs|doas)` +
	`( (-[^\s;&|]+( [^\s;&|-][^\s;&|]*)?|[A-Za-z_][A-Za-z0-9_]*=[^\s;&|]*))*) )*` +
	`([^\s;&|]*/)?\\?`

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
	"regex:" + cmdPos + `rm( [^\s;&|]+)*? (-[A-Za-z]*[rR][A-Za-z]*|--recursive)( [^\s;&|]+)*? ["']?(/\*?|~/?|\$HOME/?|\$\{HOME\}/?)["']?([\s;&|)\x60]|$)`,
	// mkfs and mkfs.<type> only when run as a command.
	"regex:" + cmdPos + `mkfs(\.[A-Za-z0-9]+)?([\s;&|)\x60]|$)`,
	":(){ :|:& };:",
	`regex:>\s*/dev/sd[a-z]`,
}
