package guard

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

// DenylistFile is the on-disk JSON representation of a denylist.
type DenylistFile struct {
	Shell []string `json:"shell"`
}

// Denylist is the compiled, ready-to-match representation of denied patterns.
type Denylist struct {
	substring []string
	regex     []*regexp.Regexp
	// sources maps each compiled pattern back to its original string for error messages.
	sources map[int]string
}

// CompileDenylist builds a Denylist from a DenylistFile. Invalid regex patterns
// are logged and skipped. CompileDenylist never returns a non-nil error.
func CompileDenylist(file DenylistFile) (*Denylist, error) {
	dl := &Denylist{
		sources: make(map[int]string),
	}

	for _, entry := range file.Shell {
		if strings.HasPrefix(entry, "regex:") {
			pattern := strings.TrimPrefix(entry, "regex:")
			re, err := regexp.Compile(pattern)
			if err != nil {
				slog.Warn("guard: skipping invalid denylist regex", "pattern", entry, "error", err)
				continue
			}
			idx := len(dl.substring) + len(dl.regex)
			dl.sources[idx] = entry
			dl.regex = append(dl.regex, re)
		} else {
			idx := len(dl.substring) + len(dl.regex)
			dl.sources[idx] = entry
			dl.substring = append(dl.substring, entry)
		}
	}

	return dl, nil
}

// Denies checks whether a tool call is blocked by the denylist.
// Only shell tool calls are checked; all other tools pass through.
// Returns (matched, pattern) where pattern is the original user-defined string.
func (d *Denylist) Denies(tool string, inputJSON []byte) (bool, string) {
	if tool != "shell" {
		return false, ""
	}

	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(inputJSON, &args); err != nil {
		return false, ""
	}

	for _, sub := range d.substring {
		if strings.Contains(args.Command, sub) {
			return true, sub
		}
	}

	for _, re := range d.regex {
		if re.MatchString(args.Command) {
			return true, fmt.Sprintf("regex:%s", re.String())
		}
	}

	return false, ""
}
