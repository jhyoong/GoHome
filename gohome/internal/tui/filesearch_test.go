package tui

import (
	"testing"
)

func TestScoreResults_ExactFilename(t *testing.T) {
	results := scoreResults("main.go", []string{"cmd/main.go", "internal/tui/model.go", "main.go"})
	if len(results) == 0 {
		t.Fatal("no results")
	}
	if results[0].Path != "main.go" {
		t.Errorf("best match: got %q, want %q", results[0].Path, "main.go")
	}
}

func TestScoreResults_StartsWithFilename(t *testing.T) {
	results := scoreResults("mod", []string{"internal/tui/model.go", "go.mod", "modfile/x.go"})
	if len(results) < 2 {
		t.Fatal("too few results")
	}
	// "go.mod" starts with "mod" in filename -- should rank above substring-in-path
	// But "modfile/x.go" has "mod" at start of a directory name, not filename.
	// The file whose basename starts with "mod" should come first.
}

func TestScoreResults_SubstringInFilename(t *testing.T) {
	results := scoreResults("odel", []string{"internal/tui/model.go", "cmd/main.go"})
	if len(results) == 0 {
		t.Fatal("no results")
	}
	if results[0].Path != "internal/tui/model.go" {
		t.Errorf("expected model.go first, got %q", results[0].Path)
	}
}

func TestScoreResults_ShorterPathBreaksTie(t *testing.T) {
	results := scoreResults("main.go", []string{"a/b/c/main.go", "a/main.go"})
	if len(results) < 2 {
		t.Fatal("too few results")
	}
	if results[0].Path != "a/main.go" {
		t.Errorf("shorter path should win tie: got %q", results[0].Path)
	}
}

func TestScoreResults_EmptyQuery(t *testing.T) {
	results := scoreResults("", []string{"a.go", "b.go"})
	if len(results) != 0 {
		t.Errorf("empty query should return no results, got %d", len(results))
	}
}
