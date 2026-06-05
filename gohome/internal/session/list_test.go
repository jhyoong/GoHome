package session

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// writeTestJSONL uses the Writer to write a synthetic JSONL file in home/sessions/<slug>/<filename>.
func writeTestJSONL(t *testing.T, home, cwd, id string, startedAt time.Time, userText string) string {
	t.Helper()
	path := SessionPath(home, cwd, id, startedAt)
	w, err := OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	w.Emit(SessionStart{ID: id, CWD: cwd, Model: "m", Endpoint: "e", Depth: 0, StartedAt: startedAt})
	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: userText}}})
	w.Emit(SessionEnd{Reason: "done"})
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return path
}

func TestListEmpty(t *testing.T) {
	home := t.TempDir()
	cwd := "/no/such/project"
	listings, err := List(home, cwd)
	if err != nil {
		t.Fatalf("List with missing dir: %v", err)
	}
	if len(listings) != 0 {
		t.Errorf("expected empty slice, got %d entries", len(listings))
	}
}

func TestListMultiple(t *testing.T) {
	home := t.TempDir()
	cwd := "/home/user/myproject"

	now := time.Now().UTC()
	older := now.Add(-24 * time.Hour)

	// Write two sessions: one older, one newer
	writeTestJSONL(t, home, cwd, "sess-old", older, "What is older?")
	writeTestJSONL(t, home, cwd, "sess-new", now, "What is newer?")

	listings, err := List(home, cwd)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listings) != 2 {
		t.Fatalf("expected 2 listings, got %d", len(listings))
	}

	// Should be sorted by StartedAt descending (most recent first)
	if listings[0].ID != "sess-new" {
		t.Errorf("listings[0].ID = %q, want %q", listings[0].ID, "sess-new")
	}
	if listings[1].ID != "sess-old" {
		t.Errorf("listings[1].ID = %q, want %q", listings[1].ID, "sess-old")
	}

	// Check Title comes from first user_message text (truncated to <=60 chars)
	if listings[0].Title != "What is newer?" {
		t.Errorf("listings[0].Title = %q, want %q", listings[0].Title, "What is newer?")
	}

	// Check path is set and exists
	if listings[0].Path == "" {
		t.Error("listings[0].Path is empty")
	}

	// Check Depth
	if listings[0].Depth != 0 {
		t.Errorf("listings[0].Depth = %d, want 0", listings[0].Depth)
	}
}

func TestListTitleTruncation(t *testing.T) {
	home := t.TempDir()
	cwd := "/home/user/proj"
	now := time.Now().UTC()
	longText := "This is a very long user message that exceeds sixty characters for sure yes it does"
	writeTestJSONL(t, home, cwd, "sess-long", now, longText)

	listings, err := List(home, cwd)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listings) != 1 {
		t.Fatalf("expected 1 listing, got %d", len(listings))
	}
	if len([]rune(listings[0].Title)) > 60 {
		t.Errorf("Title length %d > 60: %q", len([]rune(listings[0].Title)), listings[0].Title)
	}
}

func TestListDifferentCWD(t *testing.T) {
	home := t.TempDir()
	cwd1 := "/home/user/project1"
	cwd2 := "/home/user/project2"
	now := time.Now().UTC()

	writeTestJSONL(t, home, cwd1, "s1", now, "project1 message")
	writeTestJSONL(t, home, cwd2, "s2", now, "project2 message")

	l1, err := List(home, cwd1)
	if err != nil {
		t.Fatalf("List cwd1: %v", err)
	}
	l2, err := List(home, cwd2)
	if err != nil {
		t.Fatalf("List cwd2: %v", err)
	}

	if len(l1) != 1 || l1[0].ID != "s1" {
		t.Errorf("cwd1 listings wrong: %+v", l1)
	}
	if len(l2) != 1 || l2[0].ID != "s2" {
		t.Errorf("cwd2 listings wrong: %+v", l2)
	}
}

func TestListPath(t *testing.T) {
	home := t.TempDir()
	cwd := "/home/user/myproject"
	now := time.Now().UTC()
	expectedPath := writeTestJSONL(t, home, cwd, "sess-abc", now, "hello")

	listings, err := List(home, cwd)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listings) != 1 {
		t.Fatalf("expected 1 listing, got %d", len(listings))
	}
	// Normalize both paths before comparing
	gotAbs, _ := filepath.Abs(listings[0].Path)
	wantAbs, _ := filepath.Abs(expectedPath)
	if gotAbs != wantAbs {
		t.Errorf("listings[0].Path = %q, want %q", listings[0].Path, expectedPath)
	}
}
