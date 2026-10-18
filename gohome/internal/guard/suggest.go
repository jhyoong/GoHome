package guard

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Suggest returns a suggested whitelist shell pattern for a given tool call,
// or "" for non-shell tools or unparseable input.
//
// Pattern construction rule (applied after tokenizing the command on whitespace):
//
//  1. Non-shell tool -> "".
//  2. Empty command -> "".
//  3. Tokens[0] in {npm, pnpm, yarn} AND len>=2 AND tokens[1]=="run":
//     take first 3 tokens (e.g. "npm run build").
//     If tokens[1] != "run": take first 2 tokens (e.g. "npm install").
//  4. Tokens[0] in {python, python3} AND len>=2 AND tokens[1]=="-m":
//     take first 3 tokens (e.g. "python -m pytest").
//     If tokens[1] != "-m": take first 2 tokens.
//  5. Tokens[0] in {git, go, cargo, docker, kubectl, make, pip, pip3}:
//     take first 2 tokens.
//  6. Otherwise: take first 1 token only.
//
// Commands with a shell operator (see hasShellOperator) get no suggestion:
// Allows never approves them, so the rule would never match.
//
// Sudo commands (any position) suggest the full command instead, ending
// with "$" so extra arguments are not covered.
//
// Commands with control characters other than tab get no suggestion: the
// TUI cannot show such a pattern faithfully.
//
// The result is regex-escaped and always prefixed with "^".
func Suggest(tool string, inputJSON []byte) string {
	if tool != "shell" {
		return ""
	}

	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(inputJSON, &args); err != nil {
		return ""
	}

	tokens := strings.Fields(args.Command)
	if len(tokens) == 0 || hasControlChar(args.Command) || hasShellOperator(args.Command) {
		return ""
	}

	// A sudo suggestion is the full command: a "^sudo" prefix would pre-approve
	// every sudo command.
	if IsSudoCommand(args.Command) {
		return "^" + regexp.QuoteMeta(strings.TrimSpace(args.Command)) + "$"
	}

	prefix := buildPrefix(tokens)
	return "^" + regexp.QuoteMeta(prefix)
}

// hasControlChar reports whether s contains a C0 control other than tab,
// DEL, a C1 control, or a bidi control (U+061C, U+200E, U+200F,
// U+202A-U+202E, U+2066-U+2069). Bidi controls can make a command look
// different from what runs. The list matches isBidi in the tui package.
func hasControlChar(s string) bool {
	for _, r := range s {
		if (r < 0x20 && r != '\t') || (r >= 0x7f && r <= 0x9f) ||
			r == 0x061c || r == 0x200e || r == 0x200f ||
			(r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			return true
		}
	}
	return false
}

// buildPrefix selects the appropriate token prefix for a shell command.
func buildPrefix(tokens []string) string {
	cmd := tokens[0]

	switch cmd {
	case "npm", "pnpm", "yarn":
		if len(tokens) >= 3 && tokens[1] == "run" {
			return join(tokens, 3)
		}
		return join(tokens, 2)

	case "python", "python3":
		if len(tokens) >= 3 && tokens[1] == "-m" {
			return join(tokens, 3)
		}
		return join(tokens, 2)

	case "git", "go", "cargo", "docker", "kubectl", "make", "pip", "pip3":
		return join(tokens, 2)

	default:
		return tokens[0]
	}
}

// join returns the first n tokens joined by spaces, capping at len(tokens).
func join(tokens []string, n int) string {
	if n > len(tokens) {
		n = len(tokens)
	}
	return strings.Join(tokens[:n], " ")
}
