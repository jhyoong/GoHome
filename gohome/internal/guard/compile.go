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
	bash        []*regexp.Regexp
	projectPath string
	mu          sync.Mutex
}

// Compile merges global and project WhitelistFiles into a ready-to-use Whitelist.
// project entries are additive (union) on top of global entries.
// Every bash pattern is auto-anchored: a leading ^ is prepended when absent.
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

	// Merge bash patterns (global then project).
	allPatterns := make([]string, 0, len(global.Bash)+len(project.Bash))
	allPatterns = append(allPatterns, global.Bash...)
	allPatterns = append(allPatterns, project.Bash...)

	for _, pat := range allPatterns {
		anchored := pat
		if !strings.HasPrefix(anchored, "^") {
			anchored = "^" + anchored
		}
		re, err := regexp.Compile(anchored)
		if err != nil {
			slog.Warn("guard: skipping invalid bash pattern", "pattern", pat, "error", err)
			continue
		}
		wl.bash = append(wl.bash, re)
	}

	return wl, nil
}

// Allows reports whether the given tool call is permitted by the whitelist.
// For tool == "bash", inputJSON must contain {"command": "..."} and the command is
// matched against compiled bash regexes.
// For any other tool, the tool name is looked up in the tools set.
func (w *Whitelist) Allows(tool string, inputJSON []byte) bool {
	if tool == "bash" {
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(inputJSON, &args); err != nil {
			return false
		}
		w.mu.Lock()
		patterns := w.bash
		w.mu.Unlock()
		for _, re := range patterns {
			if re.MatchString(args.Command) {
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
