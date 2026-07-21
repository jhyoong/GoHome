package guard

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
)

// LoadDenylist reads global and project denylist files, merges them, and
// returns a compiled Denylist.
//
// Rules:
//   - If neither file exists (or both are malformed), built-in DefaultDenyPatterns are used.
//   - If at least one valid file exists, defaults are fully replaced by user config.
//   - When both files exist, their shell arrays are merged (union).
//   - A missing file is not an error; it is treated as absent.
//   - A malformed JSON file is logged and treated as absent.
func LoadDenylist(globalPath, projectPath string) (*Denylist, error) {
	global, globalOK := loadDenylistFile(globalPath)
	project, projectOK := loadDenylistFile(projectPath)

	// If no valid user file exists, use built-in defaults.
	if !globalOK && !projectOK {
		return CompileDenylist(DenylistFile{Shell: DefaultDenyPatterns})
	}

	// Merge global + project (union).
	merged := DenylistFile{}
	if globalOK {
		merged.Shell = append(merged.Shell, global.Shell...)
	}
	if projectOK {
		merged.Shell = append(merged.Shell, project.Shell...)
	}

	return CompileDenylist(merged)
}

// loadDenylistFile reads and decodes a DenylistFile at path.
// Returns (file, true) on success. Returns (empty, false) if missing or malformed.
func loadDenylistFile(path string) (DenylistFile, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("guard: cannot read denylist file", "path", path, "err", err)
		}
		return DenylistFile{}, false
	}
	var df DenylistFile
	if err := json.Unmarshal(data, &df); err != nil {
		slog.Warn("guard: malformed denylist JSON, treating as absent", "path", path, "err", err)
		return DenylistFile{}, false
	}
	return df, true
}
