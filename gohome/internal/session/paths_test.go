package session

import (
	"strings"
	"testing"
	"time"
)

func TestProjectSlug(t *testing.T) {
	tests := []struct {
		cwd     string
		wantPfx string // the base name
	}{
		{"/home/user/myproject", "myproject-"},
		{"/tmp/work", "work-"},
	}

	for _, tt := range tests {
		slug := ProjectSlug(tt.cwd)
		if !strings.HasPrefix(slug, tt.wantPfx) {
			t.Errorf("ProjectSlug(%q) = %q, want prefix %q", tt.cwd, slug, tt.wantPfx)
		}
		// suffix after the base name should be 6 hex chars
		suffix := slug[len(tt.wantPfx):]
		if len(suffix) != 6 {
			t.Errorf("ProjectSlug(%q): suffix %q length %d, want 6", tt.cwd, suffix, len(suffix))
		}
	}

	// Idempotent: same cwd -> same slug
	s1 := ProjectSlug("/home/user/myproject")
	s2 := ProjectSlug("/home/user/myproject")
	if s1 != s2 {
		t.Errorf("ProjectSlug not stable: %q != %q", s1, s2)
	}

	// Different cwd -> different slug (collision possible but unlikely for these)
	sa := ProjectSlug("/home/user/alpha")
	sb := ProjectSlug("/home/user/beta")
	if sa == sb {
		t.Errorf("different cwds produced the same slug: %q", sa)
	}
}

func TestSessionPath(t *testing.T) {
	home := "/home/user/.gohome"
	cwd := "/home/user/myproject"
	id := "abc123"
	ts := time.Date(2024, 3, 15, 10, 0, 0, 0, time.UTC)

	path := SessionPath(home, cwd, id, ts)

	// Should contain the date
	if !strings.Contains(path, "2024-03-15") {
		t.Errorf("SessionPath: missing date in %q", path)
	}
	// Should contain the session ID
	if !strings.Contains(path, id) {
		t.Errorf("SessionPath: missing id %q in %q", id, path)
	}
	// Should end with .jsonl
	if !strings.HasSuffix(path, ".jsonl") {
		t.Errorf("SessionPath: expected .jsonl suffix, got %q", path)
	}
	// Should be under home/sessions/<slug>/
	if !strings.HasPrefix(path, home+"/sessions/") {
		t.Errorf("SessionPath: expected prefix %q, got %q", home+"/sessions/", path)
	}
}
