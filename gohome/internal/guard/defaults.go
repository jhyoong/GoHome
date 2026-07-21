package guard

// DefaultDenyPatterns is the built-in set of shell command patterns that are
// blocked when no user-defined denylist file exists. Plain strings are substring
// matches; entries prefixed with "regex:" are full regular expressions.
var DefaultDenyPatterns = []string{
	"rm -rf /",
	"mkfs",
	":(){ :|:& };:",
	`regex:>\s*/dev/sd[a-z]`,
}
