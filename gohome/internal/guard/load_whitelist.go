package guard

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
)

// LoadWhitelist reads both whitelist.json files, merges them, and returns a
// compiled Whitelist ready for use.
//
// Rules:
//   - A missing file is not an error; it is treated as an empty WhitelistFile.
//   - A malformed JSON file is logged via slog.Warn and treated as empty.
//   - projectPath is forwarded to Compile so that AddProject can persist entries.
func LoadWhitelist(globalPath, projectPath string) (*Whitelist, error) {
	global := loadWhitelistFile(globalPath)
	project := loadWhitelistFile(projectPath)
	return Compile(global, project, projectPath)
}

// loadWhitelistFile reads and decodes a WhitelistFile at path.
// Missing file -> empty WhitelistFile (no error).
// Malformed JSON -> slog.Warn + empty WhitelistFile (no error).
func loadWhitelistFile(path string) WhitelistFile {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("guard: cannot read whitelist file", "path", path, "err", err)
		}
		return WhitelistFile{}
	}
	var wf WhitelistFile
	if err := json.Unmarshal(data, &wf); err != nil {
		slog.Warn("guard: malformed whitelist JSON, treating as empty", "path", path, "err", err)
		return WhitelistFile{}
	}
	return wf
}
