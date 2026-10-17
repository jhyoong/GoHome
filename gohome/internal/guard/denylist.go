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
	substring []substringEntry
	regex     []*regexp.Regexp
	// sources maps each compiled pattern back to its original string for error messages.
	sources map[int]string
}

// substringEntry is a plain denylist entry. squeezed is the form used for
// matching; original is reported back to the user.
type substringEntry struct {
	original string
	squeezed string
}

// spaceRun matches runs of spaces and tabs.
var spaceRun = regexp.MustCompile(`[ \t]+`)

// squeeze collapses runs of spaces and tabs into a single space.
func squeeze(s string) string {
	return spaceRun.ReplaceAllString(s, " ")
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
			dl.substring = append(dl.substring, substringEntry{original: entry, squeezed: squeeze(entry)})
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

	// Squeeze spaces and tabs so "rm  -rf /" matches like "rm -rf /".
	cmd := squeeze(args.Command)

	for _, sub := range d.substring {
		if strings.Contains(cmd, sub.squeezed) {
			return true, sub.original
		}
	}

	for _, re := range d.regex {
		if re.MatchString(cmd) {
			return true, fmt.Sprintf("regex:%s", re.String())
		}
	}

	return false, ""
}
