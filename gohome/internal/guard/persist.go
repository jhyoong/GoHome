package guard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// AddProject persists a new tool or shell pattern into the project whitelist file
// and updates the in-memory Whitelist atomically.
//
// For tool == "shell": pattern is appended to the shell list (no-op if already present).
// For any other tool: tool is appended to the tools list (no-op if already present).
//
// The in-process sync.Mutex (w.mu) guards the whole read-modify-write together with
// an OS-level file lock so concurrent processes also stay consistent.
//
// A missing project file (or its parent directories) is created on demand.
// A missing projectPath is a no-op for the file portion; in-memory state is
// still updated.
func (w *Whitelist) AddProject(tool, pattern string) error {
	// Validate and normalize before touching memory or the file.
	if tool == "shell" {
		var err error
		pattern, err = NormalizePattern(pattern)
		if err != nil {
			return err
		}
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// --- update in-memory state first ---
	if tool == "shell" {
		re, err := compileShellPattern(pattern)
		if err != nil {
			return err
		}
		// Only add if not already present.
		alreadyPresent := false
		for _, have := range w.shell {
			if have.String() == re.String() {
				alreadyPresent = true
				break
			}
		}
		if !alreadyPresent {
			w.shell = append(w.shell, re)
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
	dir := filepath.Dir(w.projectPath)
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
	if tool == "shell" {
		if !containsPattern(wf.Shell, pattern) {
			wf.Shell = append(wf.Shell, pattern)
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

// NormalizePattern returns pattern in its one stored form (anchored with "^").
// It rejects a pattern that is empty or whitespace-only (with or without the
// "^"), since that would match every command, and a pattern that is not a
// valid regex.
func NormalizePattern(pattern string) (string, error) {
	anchored := pattern
	if !strings.HasPrefix(anchored, "^") {
		anchored = "^" + anchored
	}
	if strings.TrimSpace(strings.TrimPrefix(anchored, "^")) == "" {
		return "", errors.New("pattern is empty")
	}
	if _, err := regexp.Compile(anchored); err != nil {
		return "", fmt.Errorf("invalid pattern: %w", err)
	}
	return anchored, nil
}

// compileShellPattern compiles pattern for matching. The normalized pattern is
// wrapped as ^(?:...) so the anchor covers the whole pattern, including
// top-level alternation ("ls|rm" must not match "rm" anywhere in a command).
func compileShellPattern(pattern string) (*regexp.Regexp, error) {
	norm, err := NormalizePattern(pattern)
	if err != nil {
		return nil, err
	}
	return regexp.Compile("^(?:" + strings.TrimPrefix(norm, "^") + ")")
}

// containsPattern reports whether slice already holds pattern, comparing the
// normalized forms so "ls" and "^ls" count as the same rule.
func containsPattern(slice []string, pattern string) bool {
	for _, v := range slice {
		if n, err := NormalizePattern(v); err == nil && n == pattern {
			return true
		}
	}
	return false
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
