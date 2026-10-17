package session

import (
	"os"
	"path/filepath"
	"strings"
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
	w.Emit(SessionStart{ID: id, CWD: cwd, Model: "m", ModelConfig: "e", Depth: 0, StartedAt: startedAt})
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

// writeBlankJSONL writes a JSONL session file with no user_message events.
func writeBlankJSONL(t *testing.T, home, cwd, id string, startedAt time.Time) string {
	t.Helper()
	path := SessionPath(home, cwd, id, startedAt)
	w, err := OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	w.Emit(SessionStart{ID: id, CWD: cwd, Model: "m", ModelConfig: "e", Depth: 0, StartedAt: startedAt})
	w.Emit(SessionEnd{Reason: "user_quit"})
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return path
}

func TestIsBlankTrue(t *testing.T) {
	home := t.TempDir()
	cwd := "/test/project"
	now := time.Now().UTC()
	path := writeBlankJSONL(t, home, cwd, "blank1", now)
	blank, err := isBlank(path)
	if err != nil {
		t.Fatalf("isBlank: %v", err)
	}
	if !blank {
		t.Error("expected blank session to be blank")
	}
}

func TestIsBlankFalse(t *testing.T) {
	home := t.TempDir()
	cwd := "/test/project"
	now := time.Now().UTC()
	path := writeTestJSONL(t, home, cwd, "used1", now, "hello world")
	blank, err := isBlank(path)
	if err != nil {
		t.Fatalf("isBlank: %v", err)
	}
	if blank {
		t.Error("expected session with user_message to not be blank")
	}
}

func TestCleanBlank(t *testing.T) {
	home := t.TempDir()
	cwd := "/test/project"
	now := time.Now().UTC()

	writeBlankJSONL(t, home, cwd, "blank1", now)
	writeBlankJSONL(t, home, cwd, "blank2", now.Add(-time.Hour))
	usedPath := writeTestJSONL(t, home, cwd, "used1", now, "hello")

	removed, err := cleanBlank(home, cwd)
	if err != nil {
		t.Fatalf("cleanBlank: %v", err)
	}
	if removed != 2 {
		t.Errorf("expected 2 removed, got %d", removed)
	}

	// The used session should still exist.
	if _, err := os.Stat(usedPath); err != nil {
		t.Errorf("used session should still exist: %v", err)
	}

	// Listing should only show the used session.
	listings, err := List(home, cwd)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listings) != 1 {
		t.Fatalf("expected 1 listing after cleanup, got %d", len(listings))
	}
	if listings[0].ID != "used1" {
		t.Errorf("remaining listing ID = %q, want %q", listings[0].ID, "used1")
	}
}

func TestCleanBlankNoDir(t *testing.T) {
	home := t.TempDir()
	removed, err := cleanBlank(home, "/nonexistent/project")
	if err != nil {
		t.Fatalf("cleanBlank should not error on missing dir: %v", err)
	}
	if removed != 0 {
		t.Errorf("expected 0 removed, got %d", removed)
	}
}

func TestListLongLine(t *testing.T) {
	home := t.TempDir()
	cwd := "/tmp/proj"
	path := SessionPath(home, cwd, "big", time.Now().UTC())
	w, err := OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	w.Emit(SessionStart{ID: "big", CWD: cwd, Model: "m", ModelConfig: "e", StartedAt: time.Now().UTC()})
	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: "hello"}}})
	w.Emit(ToolResult{ToolUseID: "tu1", Content: strings.Repeat("x", 70*1024)})
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got, err := List(home, cwd)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != "big" {
		t.Fatalf("List = %+v, want the big session", got)
	}
}

// writeChildJSONL writes a depth-1 subagent session file linked to parentID.
func writeChildJSONL(t *testing.T, home, cwd, id, parentID string, startedAt time.Time, task string) string {
	t.Helper()
	path := SessionPath(home, cwd, id, startedAt)
	w, err := OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	w.Emit(SessionStart{ID: id, ParentID: parentID, CWD: cwd, Model: "m", ModelConfig: "e", Depth: 1, StartedAt: startedAt})
	w.Emit(UserMessage{Content: []common.Block{{Kind: common.BlockText, Text: task}}})
	w.Emit(SessionEnd{Reason: "done"})
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return path
}

func TestListExcludesChildren(t *testing.T) {
	home := t.TempDir()
	cwd := "/home/user/myproject"
	now := time.Now().UTC()

	writeTestJSONL(t, home, cwd, "parent", now.Add(-time.Minute), "parent task")
	// The child starts after its parent, so without filtering it sorts first.
	writeChildJSONL(t, home, cwd, "parent-sub-1", "parent", now, "child task")

	listings, err := List(home, cwd)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listings) != 1 || listings[0].ID != "parent" {
		t.Fatalf("List = %+v, want only the parent session", listings)
	}
}

func TestListChildren(t *testing.T) {
	home := t.TempDir()
	cwd := "/home/user/myproject"
	now := time.Now().UTC()

	writeTestJSONL(t, home, cwd, "p1", now.Add(-time.Hour), "parent one")
	writeTestJSONL(t, home, cwd, "p2", now.Add(-time.Hour), "parent two")
	// Written out of order to check sorting by StartedAt ascending.
	writeChildJSONL(t, home, cwd, "p1-sub-2", "p1", now.Add(-10*time.Minute), "second")
	writeChildJSONL(t, home, cwd, "p1-sub-1", "p1", now.Add(-20*time.Minute), "first")
	writeChildJSONL(t, home, cwd, "p2-sub-1", "p2", now.Add(-15*time.Minute), "other parent")

	children, err := ListChildren(home, cwd, "p1")
	if err != nil {
		t.Fatalf("ListChildren: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("ListChildren len = %d, want 2: %+v", len(children), children)
	}
	if children[0].ID != "p1-sub-1" || children[1].ID != "p1-sub-2" {
		t.Errorf("ListChildren order = [%s %s], want [p1-sub-1 p1-sub-2]", children[0].ID, children[1].ID)
	}
	if children[0].ParentID != "p1" || children[0].Depth != 1 {
		t.Errorf("child listing = %+v, want ParentID p1 and Depth 1", children[0])
	}
}

func TestListChildrenNoDir(t *testing.T) {
	children, err := ListChildren(t.TempDir(), "/no/such/project", "p1")
	if err != nil {
		t.Fatalf("ListChildren: %v", err)
	}
	if len(children) != 0 {
		t.Errorf("expected no children, got %d", len(children))
	}
}

func TestLoadChildren(t *testing.T) {
	home := t.TempDir()
	cwd := "/home/user/myproject"
	now := time.Now().UTC()

	writeTestJSONL(t, home, cwd, "p1", now.Add(-time.Hour), "parent")
	writeChildJSONL(t, home, cwd, "p1-sub-2", "p1", now.Add(-10*time.Minute), "second")
	writeChildJSONL(t, home, cwd, "p1-sub-1", "p1", now.Add(-20*time.Minute), "first")

	children, err := LoadChildren(home, cwd, "p1")
	if err != nil {
		t.Fatalf("LoadChildren: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("LoadChildren len = %d, want 2", len(children))
	}
	if children[0].ID != "p1-sub-1" || children[1].ID != "p1-sub-2" {
		t.Errorf("LoadChildren order = [%s %s], want [p1-sub-1 p1-sub-2]", children[0].ID, children[1].ID)
	}
	if len(children[0].History) != 1 || children[0].History[0].Content[0].Text != "first" {
		t.Errorf("child history = %+v, want the task message", children[0].History)
	}
}
