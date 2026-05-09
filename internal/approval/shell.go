package approval

import (
	"encoding/json"
	"strings"
)

// isChainedCommand returns true if cmd contains shell chaining or piping
// operators (&&, ||, ;, |) outside of single or double quotes.
func isChainedCommand(cmd string) bool {
	inSingle, inDouble := false, false
	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]
		switch {
		case ch == '\'' && !inDouble:
			inSingle = !inSingle
		case ch == '"' && !inSingle:
			inDouble = !inDouble
		case inSingle || inDouble:
			// inside quotes — skip
		case ch == '|', ch == ';':
			return true
		case ch == '&' && i+1 < len(cmd) && cmd[i+1] == '&':
			return true
		}
	}
	return false
}

// matchGlob matches s against pattern where * matches any sequence of characters.
func matchGlob(pattern, s string) bool {
	if pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return pattern == s
	}
	idx := strings.Index(pattern, "*")
	prefix := pattern[:idx]
	rest := pattern[idx+1:]
	if !strings.HasPrefix(s, prefix) {
		return false
	}
	s = s[len(prefix):]
	if rest == "" {
		return true
	}
	for i := 0; i <= len(s); i++ {
		if matchGlob(rest, s[i:]) {
			return true
		}
	}
	return false
}

// extractShellCommand parses the "command" field from shell tool params JSON.
func extractShellCommand(params json.RawMessage) string {
	var p struct {
		Command string `json:"command"`
	}
	json.Unmarshal(params, &p) //nolint:errcheck — empty string on failure is safe
	return p.Command
}
