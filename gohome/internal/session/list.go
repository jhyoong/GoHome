package session

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Listing holds metadata about a previously persisted session.
type Listing struct {
	Path       string    `json:"path"`
	ID         string    `json:"id"`
	StartedAt  time.Time `json:"startedAt"`
	LastActive time.Time `json:"lastActive"`
	Title      string    `json:"title"`
	Depth      int       `json:"depth"`
	ParentID   string    `json:"parentId"`
}

// List returns the top-level persisted sessions stored under home for the
// given cwd. Subagent sessions (Depth > 0) are skipped; see ListChildren.
// Sessions are sorted by StartedAt descending (most recent first).
// If the sessions directory does not exist, an empty slice is returned with no error.
func List(home, cwd string) ([]Listing, error) {
	all, err := listAll(home, cwd)
	if err != nil {
		return nil, err
	}
	var listings []Listing
	for _, l := range all {
		if l.Depth == 0 {
			listings = append(listings, l)
		}
	}
	sort.Slice(listings, func(i, j int) bool {
		return listings[i].StartedAt.After(listings[j].StartedAt)
	})
	return listings, nil
}

// ListChildren returns the subagent sessions whose ParentID is parentID,
// sorted by StartedAt ascending (the order they were spawned).
func ListChildren(home, cwd, parentID string) ([]Listing, error) {
	all, err := listAll(home, cwd)
	if err != nil {
		return nil, err
	}
	var children []Listing
	for _, l := range all {
		if l.Depth > 0 && l.ParentID == parentID {
			children = append(children, l)
		}
	}
	sort.SliceStable(children, func(i, j int) bool {
		return children[i].StartedAt.Before(children[j].StartedAt)
	})
	return children, nil
}

// LoadChildren loads every subagent session of parentID, in spawn order.
// Files that fail to load are skipped with a warning.
func LoadChildren(home, cwd, parentID string) ([]*Session, error) {
	listings, err := ListChildren(home, cwd, parentID)
	if err != nil {
		return nil, err
	}
	var children []*Session
	for _, l := range listings {
		child, _, err := Load(l.Path)
		if err != nil {
			slog.Warn("session: skipping unloadable subagent session", "path", l.Path, "err", err)
			continue
		}
		children = append(children, child)
	}
	return children, nil
}

// listAll parses every session file under home for cwd, unsorted.
func listAll(home, cwd string) ([]Listing, error) {
	dir := filepath.Join(home, "sessions", ProjectSlug(cwd))
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var listings []Listing
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		listing, err := parseListing(path)
		if err != nil {
			// Skip unreadable/malformed files rather than failing the list.
			slog.Warn("session: skipping unreadable file", "path", path, "err", err)
			continue
		}
		listings = append(listings, listing)
	}
	return listings, nil
}

// parseListing reads a JSONL file in a single pass to build a Listing.
// session_start gives the metadata, the first user_message the title, and the
// last timestamp seen on any line gives LastActive.
func parseListing(path string) (Listing, error) {
	f, err := os.Open(path)
	if err != nil {
		return Listing{}, err
	}
	defer func() { _ = f.Close() }()

	var listing Listing
	listing.Path = path

	var lastTS string
	foundTitle := false

	err = forEachLine(f, func(raw []byte) {
		var envelope struct {
			Type string `json:"type"`
			TS   string `json:"ts"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return
		}
		if envelope.TS != "" {
			lastTS = envelope.TS
		}

		switch envelope.Type {
		case "session_start":
			var ev struct {
				ID        string    `json:"id"`
				Depth     int       `json:"depth"`
				TS        string    `json:"ts"`
				ParentID  string    `json:"parentId"`
				StartedAt time.Time `json:"startedAt"`
			}
			if err := json.Unmarshal(raw, &ev); err == nil {
				listing.ID = ev.ID
				listing.Depth = ev.Depth
				listing.ParentID = ev.ParentID
				if !ev.StartedAt.IsZero() {
					listing.StartedAt = ev.StartedAt
				} else if t, err := time.Parse(time.RFC3339, ev.TS); err == nil {
					listing.StartedAt = t
				}
			}

		case "user_message":
			if !foundTitle {
				var ev struct {
					Content []struct {
						Kind string `json:"kind"`
						Text string `json:"text"`
					} `json:"content"`
				}
				if err := json.Unmarshal(raw, &ev); err == nil {
					for _, blk := range ev.Content {
						if blk.Kind == "text" && blk.Text != "" {
							listing.Title = truncate(blk.Text, 60)
							foundTitle = true
							break
						}
					}
				}
			}
		}
	})
	if err != nil {
		return Listing{}, err
	}

	if listing.ID == "" {
		return Listing{}, fmt.Errorf("no session_start found in %s", path)
	}

	if lastTS != "" {
		if t, err := time.Parse(time.RFC3339, lastTS); err == nil {
			listing.LastActive = t
		}
	}

	return listing, nil
}

// isBlank reports whether the JSONL file at path contains no user_message events.
func isBlank(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	return blankReader(f)
}

// blankReader reports whether r contains no user_message events. It stops
// reading at the first one.
func blankReader(r io.Reader) (bool, error) {
	blank := true
	err := forEachLineUntil(r, func(raw []byte) bool {
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return true
		}
		if envelope.Type == "user_message" {
			blank = false
		}
		return blank
	})
	if err != nil {
		return false, err
	}
	return blank, nil
}

// cleanBlank removes all session JSONL files under home/sessions/<slug> that
// contain no user_message events. Returns the number of files removed.
func cleanBlank(home, cwd string) (int, error) {
	dir := filepath.Join(home, "sessions", ProjectSlug(cwd))
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	removed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		blank, err := isBlank(path)
		if err != nil {
			continue
		}
		if blank {
			if err := os.Remove(path); err == nil {
				removed++
			}
		}
	}
	return removed, nil
}

// CleanBlankSessions removes all session JSONL files under home/sessions/<slug>
// that contain no user_message events. Returns the number of files removed.
func CleanBlankSessions(home, cwd string) (int, error) {
	return cleanBlank(home, cwd)
}

// truncate returns s truncated to at most n runes.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
