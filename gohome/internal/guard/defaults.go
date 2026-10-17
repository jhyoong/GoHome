package guard

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
	`regex:(^|[\s;&|(/])rm( [^\s;&|]+)*? (-[A-Za-z]*[rR][A-Za-z]*|--recursive)( [^\s;&|]+)*? ["']?(/\*?|~/?|\$HOME/?|\$\{HOME\}/?)["']?([\s;&|)]|$)`,
	// mkfs and mkfs.<type> only when run as a command.
	`regex:(^|[;&|(] ?|sudo |/)mkfs(\.[A-Za-z0-9]+)?([\s;&|)]|$)`,
	":(){ :|:& };:",
	`regex:>\s*/dev/sd[a-z]`,
}
