package approval

import "testing"

func TestIsChainedCommand(t *testing.T) {
	tests := []struct {
		cmd     string
		chained bool
	}{
		{"ls -la /tmp", false},
		{"ls -la /tmp && rm -rf /", true},
		{"ls | grep foo", true},
		{"ls || echo fail", true},
		{"ls; echo done", true},
		{`echo "hello && world"`, false},
		{`echo 'hello | world'`, false},
		{"", false},
	}
	for _, tc := range tests {
		got := isChainedCommand(tc.cmd)
		if got != tc.chained {
			t.Errorf("isChainedCommand(%q) = %v, want %v", tc.cmd, got, tc.chained)
		}
	}
}

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		s       string
		match   bool
	}{
		{"ls *", "ls -la", true},
		{"ls *", "ls -la /tmp", true},
		{"ls *", "cat /etc/passwd", false},
		{"git commit *", "git commit -m message", true},
		{"git commit *", "git status", false},
		{"*", "anything at all", true},
		{"exact", "exact", true},
		{"exact", "exact2", false},
		{"exact", "notexact", false},
		{"pre*suf", "preMIDDLEsuf", true},
		{"pre*suf", "presuf", true},
		{"pre*suf", "preMIDDLE", false},
	}
	for _, tc := range tests {
		got := matchGlob(tc.pattern, tc.s)
		if got != tc.match {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tc.pattern, tc.s, got, tc.match)
		}
	}
}

func TestExtractShellCommand(t *testing.T) {
	got := extractShellCommand([]byte(`{"command":"ls -la /tmp"}`))
	if got != "ls -la /tmp" {
		t.Errorf("got %q, want %q", got, "ls -la /tmp")
	}
	if extractShellCommand([]byte(`{}`)) != "" {
		t.Error("expected empty string for missing command field")
	}
}
