package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Listing holds metadata about a previously persisted session.
type Listing struct {
	Path       string
	ID         string
	StartedAt  time.Time
	LastActive time.Time
	Title      string // first user_message text block, truncated to <=60 runes
	Depth      int
}

// List returns all resumable sessions stored under home for the given cwd.
// Sessions are sorted by StartedAt descending (most recent first).
// If the sessions directory does not exist, an empty slice is returned with no error.
func List(home, cwd string) ([]Listing, error) {
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
			// Skip unreadable/malformed files rather than surfacing an error.
			continue
		}
		listings = append(listings, listing)
	}

	sort.Slice(listings, func(i, j int) bool {
		return listings[i].StartedAt.After(listings[j].StartedAt)
	})
	return listings, nil
}

// parseListing reads a JSONL file in a single pass to build a Listing.
// It reads all lines: session_start for metadata, first user_message for title,
// last line for LastActive timestamp.
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

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var envelope struct {
			Type string `json:"type"`
			TS   string `json:"ts"`
		}
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			continue
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
			if err := json.Unmarshal([]byte(line), &ev); err == nil {
				listing.ID = ev.ID
				listing.Depth = ev.Depth
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
				if err := json.Unmarshal([]byte(line), &ev); err == nil {
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
	}
	if err := scanner.Err(); err != nil {
		return Listing{}, err
	}

	if lastTS != "" {
		if t, err := time.Parse(time.RFC3339, lastTS); err == nil {
			listing.LastActive = t
		}
	}

	return listing, nil
}

// truncate returns s truncated to at most n runes.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
