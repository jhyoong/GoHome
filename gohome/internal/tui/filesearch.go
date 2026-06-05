package tui

import (
	"path/filepath"
	"sort"
	"strings"
)

// ScoredResult is a file path with a relevance score.
// Exported so that tests in tui_test (external package) can construct them.
type ScoredResult struct {
	Path  string
	Score int
}

func scoreResults(query string, paths []string) []ScoredResult {
	if query == "" {
		return nil
	}
	q := strings.ToLower(query)
	var results []ScoredResult
	for _, p := range paths {
		base := strings.ToLower(filepath.Base(p))
		lp := strings.ToLower(p)
		var score int
		switch {
		case base == q:
			score = 0
		case strings.HasPrefix(base, q):
			score = 20
		case strings.Contains(base, q):
			score = 50
		case strings.Contains(lp, q):
			score = 70
		default:
			continue
		}
		results = append(results, ScoredResult{Path: p, Score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score < results[j].Score
		}
		return len(results[i].Path) < len(results[j].Path)
	})
	return results
}
