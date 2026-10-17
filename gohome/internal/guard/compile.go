package guard

import (
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
	"sync"
)

// Whitelist is the compiled, in-memory representation of merged global + project whitelists.
type Whitelist struct {
	tools       map[string]struct{}
	shell       []*regexp.Regexp
	projectPath string
	mu          sync.Mutex
}

// Compile merges global and project WhitelistFiles into a ready-to-use Whitelist.
// project entries are additive (union) on top of global entries.
// Every shell pattern is auto-anchored: a leading ^ is prepended when absent.
// Invalid regex patterns are logged via slog.Warn and skipped; Compile never returns a
// non-nil error (signature kept for forward-compatibility and clarity).
func Compile(global, project WhitelistFile, projectPath string) (*Whitelist, error) {
	wl := &Whitelist{
		tools:       make(map[string]struct{}),
		projectPath: projectPath,
	}

	// Merge tool sets (global then project).
	for _, t := range global.Tools {
		wl.tools[t] = struct{}{}
	}
	for _, t := range project.Tools {
		wl.tools[t] = struct{}{}
	}

	// Merge shell patterns (global then project).
	allPatterns := make([]string, 0, len(global.Shell)+len(project.Shell))
	allPatterns = append(allPatterns, global.Shell...)
	allPatterns = append(allPatterns, project.Shell...)

	for _, pat := range allPatterns {
		re, err := compileShellPattern(pat)
		if err != nil {
			slog.Warn("guard: skipping invalid shell pattern", "pattern", pat, "error", err)
			continue
		}
		wl.shell = append(wl.shell, re)
	}

	return wl, nil
}

// Allows reports whether the given tool call is permitted by the whitelist.
// For tool == "shell", inputJSON must contain {"command": "..."} and the command is
// matched against compiled shell regexes.
// For any other tool, the tool name is looked up in the tools set.
func (w *Whitelist) Allows(tool string, inputJSON []byte) bool {
	if tool == "shell" {
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(inputJSON, &args); err != nil {
			return false
		}
		w.mu.Lock()
		patterns := w.shell
		w.mu.Unlock()
		if hasShellOperator(args.Command) {
			return false
		}
		for _, re := range patterns {
			if matchesWholeWord(re, args.Command) {
				return true
			}
		}
		return false
	}

	w.mu.Lock()
	_, ok := w.tools[tool]
	w.mu.Unlock()
	return ok
}

// hasShellOperator reports whether cmd contains a character sequence that can
// chain, substitute, or redirect commands. Such commands are never auto-approved.
func hasShellOperator(cmd string) bool {
	return strings.ContainsAny(cmd, ";&|\n`<>") || strings.Contains(cmd, "$(")
}

// matchesWholeWord reports whether re matches a prefix of cmd that ends at a
// word boundary: the end of the command, a following space, or a match that
// itself ends in a space (e.g. the pattern "^git ").
func matchesWholeWord(re *regexp.Regexp, cmd string) bool {
	loc := re.FindStringIndex(cmd)
	if loc == nil {
		return false
	}
	end := loc[1]
	return end == len(cmd) || cmd[end] == ' ' || (end > 0 && cmd[end-1] == ' ')
}
