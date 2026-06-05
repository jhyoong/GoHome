package guard

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
)

// AddProject persists a new tool or bash pattern into the project whitelist file
// and updates the in-memory Whitelist atomically.
//
// For tool == "bash": pattern is appended to the bash list (no-op if already present).
// For any other tool: tool is appended to the tools list (no-op if already present).
//
// The in-process sync.Mutex (w.mu) guards the whole read-modify-write together with
// an OS-level file lock so concurrent processes also stay consistent.
//
// A missing project file (or its parent directories) is created on demand.
// A missing projectPath is a no-op for the file portion; in-memory state is
// still updated.
func (w *Whitelist) AddProject(tool, pattern string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// --- update in-memory state first ---
	if tool == "bash" {
		anchored := pattern
		if !strings.HasPrefix(anchored, "^") {
			anchored = "^" + anchored
		}
		// Only add if not already matched by an existing pattern.
		alreadyPresent := false
		for _, re := range w.bash {
			if re.String() == anchored {
				alreadyPresent = true
				break
			}
		}
		if !alreadyPresent {
			re, err := regexp.Compile(anchored)
			if err == nil {
				w.bash = append(w.bash, re)
			}
		}
	} else {
		w.tools[tool] = struct{}{}
	}

	// --- persist to file if a path is configured ---
	if w.projectPath == "" {
		return nil
	}

	return w.persistToFile(tool, pattern)
}

// persistToFile reads the project whitelist file, merges the new entry, and writes it back.
// Must be called with w.mu already held.
func (w *Whitelist) persistToFile(tool, pattern string) error {
	// Ensure parent directories exist.
	dir := dirOf(w.projectPath)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	// Open (or create) the file for read+write.
	f, err := os.OpenFile(w.projectPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	// Acquire OS-level exclusive file lock.
	if err := lockFile(f); err != nil {
		return err
	}
	defer unlockFile(f) //nolint:errcheck

	// Read existing content.
	var wf WhitelistFile
	info, _ := f.Stat()
	if info != nil && info.Size() > 0 {
		dec := json.NewDecoder(f)
		if err := dec.Decode(&wf); err != nil {
			return err
		}
	}

	// Merge new entry (idempotent).
	if tool == "bash" {
		if !containsStr(wf.Bash, pattern) {
			wf.Bash = append(wf.Bash, pattern)
		}
	} else {
		if !containsStr(wf.Tools, tool) {
			wf.Tools = append(wf.Tools, tool)
		}
	}

	// Truncate and rewrite.
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(wf)
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// dirOf returns the directory component of a file path.
func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return ""
}
