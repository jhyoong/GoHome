package guard

import (
	"encoding/json"
	"testing"
)

func TestCompileAllows_Tool(t *testing.T) {
	global := WhitelistFile{Tools: []string{"read", "write"}}
	project := WhitelistFile{Tools: []string{"edit"}}

	wl, err := Compile(global, project, "")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	cases := []struct {
		tool  string
		input []byte
		want  bool
	}{
		{"read", nil, true},
		{"write", nil, true},
		{"edit", nil, true},    // project adds to global
		{"delete", nil, false}, // not in either
	}
	for _, c := range cases {
		got := wl.Allows(c.tool, c.input)
		if got != c.want {
			t.Errorf("Allows(%q) = %v, want %v", c.tool, got, c.want)
		}
	}
}

func TestCompileAllows_Bash(t *testing.T) {
	global := WhitelistFile{Bash: []string{"^git status"}}
	project := WhitelistFile{Bash: []string{"^ls"}}

	wl, err := Compile(global, project, "")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	bashInput := func(cmd string) []byte {
		b, _ := json.Marshal(map[string]string{"command": cmd})
		return b
	}

	cases := []struct {
		cmd  string
		want bool
	}{
		{"git status -sb", true},     // global pattern
		{"ls -la /tmp", true},        // project pattern
		{"rm -rf /", false},          // no matching pattern
		{"git log --oneline", false}, // doesn't match ^git status
	}
	for _, c := range cases {
		got := wl.Allows("bash", bashInput(c.cmd))
		if got != c.want {
			t.Errorf("Allows(bash, %q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

func TestCompileAutoAnchor(t *testing.T) {
	// Pattern without ^ is auto-anchored so "ls" matches "ls -la" but NOT "pls"
	global := WhitelistFile{Bash: []string{"ls"}} // no ^ prefix
	wl, err := Compile(global, WhitelistFile{}, "")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	bashInput := func(cmd string) []byte {
		b, _ := json.Marshal(map[string]string{"command": cmd})
		return b
	}

	if !wl.Allows("bash", bashInput("ls -la")) {
		t.Error("expected 'ls -la' to be allowed by auto-anchored 'ls' pattern")
	}
	if wl.Allows("bash", bashInput("pls")) {
		t.Error("expected 'pls' to be denied by auto-anchored 'ls' pattern")
	}
}

func TestCompileBadRegex(t *testing.T) {
	// Bad regex should be skipped; valid entries still load
	global := WhitelistFile{Bash: []string{"[invalid", "^git status"}}
	wl, err := Compile(global, WhitelistFile{}, "")
	if err != nil {
		t.Fatalf("Compile should not return error for bad regex, got: %v", err)
	}

	bashInput := func(cmd string) []byte {
		b, _ := json.Marshal(map[string]string{"command": cmd})
		return b
	}

	// Good pattern still works
	if !wl.Allows("bash", bashInput("git status")) {
		t.Error("expected valid pattern to still work after bad regex is skipped")
	}
}
