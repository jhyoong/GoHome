package session

import (
	"crypto/sha1"
	"fmt"
	"path/filepath"
	"time"
)

// ProjectSlug returns a stable, filesystem-safe identifier for cwd.
// Format: <basename>-<first 6 hex chars of sha1(cwd)>
func ProjectSlug(cwd string) string {
	base := filepath.Base(cwd)
	sum := sha1.Sum([]byte(cwd))
	return fmt.Sprintf("%s-%x", base, sum[:3]) // 3 bytes = 6 hex chars
}

// SessionPath returns the canonical on-disk path for a session JSONL file.
// Format: <home>/sessions/<slug>/<date>-<sessionID>.jsonl
func SessionPath(home, cwd, sessionID string, t time.Time) string {
	slug := ProjectSlug(cwd)
	filename := t.Format("2006-01-02") + "-" + sessionID + ".jsonl"
	return filepath.Join(home, "sessions", slug, filename)
}
