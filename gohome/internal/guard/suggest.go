package guard

import (
	"encoding/json"
	"strings"
)

// Suggest returns a suggested whitelist bash pattern for a given tool call,
// or "" for non-bash tools or unparseable input.
//
// Pattern construction rule (applied after tokenizing the command on whitespace):
//
//  1. Non-bash tool -> "".
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
// The result is always prefixed with "^".
func Suggest(tool string, inputJSON []byte) string {
	if tool != "bash" {
		return ""
	}

	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(inputJSON, &args); err != nil {
		return ""
	}

	tokens := strings.Fields(args.Command)
	if len(tokens) == 0 {
		return ""
	}

	prefix := buildPrefix(tokens)
	return "^" + prefix
}

// buildPrefix selects the appropriate token prefix for a bash command.
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
