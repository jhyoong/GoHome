package guard

import (
	"encoding/json"
	"testing"
)

func shellInput(cmd string) []byte {
	b, _ := json.Marshal(map[string]string{"command": cmd})
	return b
}

func TestCompileDenylist_SubstringMatch(t *testing.T) {
	dl, err := CompileDenylist(DenylistFile{Shell: []string{"rm -rf /"}})
	if err != nil {
		t.Fatalf("CompileDenylist: %v", err)
	}

	matched, pattern := dl.Denies("shell", shellInput("rm -rf /"))
	if !matched {
		t.Error("expected 'rm -rf /' to be denied")
	}
	if pattern != "rm -rf /" {
		t.Errorf("pattern = %q, want %q", pattern, "rm -rf /")
	}
}

func TestCompileDenylist_SubstringInPipedCommand(t *testing.T) {
	dl, _ := CompileDenylist(DenylistFile{Shell: []string{"rm -rf /"}})

	cases := []struct {
		cmd  string
		want bool
	}{
		{"echo foo && rm -rf /", true},
		{"cat file | xargs rm -rf /", true},
		{"echo hello; rm -rf / ; echo done", true},
		{"ls -la", false},
	}
	for _, c := range cases {
		matched, _ := dl.Denies("shell", shellInput(c.cmd))
		if matched != c.want {
			t.Errorf("Denies(%q) = %v, want %v", c.cmd, matched, c.want)
		}
	}
}

func TestCompileDenylist_RegexMatch(t *testing.T) {
	dl, _ := CompileDenylist(DenylistFile{Shell: []string{`regex:>\s*/dev/sd[a-z]`}})

	cases := []struct {
		cmd  string
		want bool
	}{
		{"echo x > /dev/sda", true},
		{"echo x >  /dev/sdz", true},
		{"cat /dev/sda", false},
	}
	for _, c := range cases {
		matched, _ := dl.Denies("shell", shellInput(c.cmd))
		if matched != c.want {
			t.Errorf("Denies(%q) = %v, want %v", c.cmd, matched, c.want)
		}
	}
}

func TestCompileDenylist_NonShellTool_Passthrough(t *testing.T) {
	dl, _ := CompileDenylist(DenylistFile{Shell: []string{"rm -rf /"}})

	matched, _ := dl.Denies("write", []byte(`{"path": "rm -rf /"}`))
	if matched {
		t.Error("expected non-shell tool to pass through denylist")
	}
}

func TestCompileDenylist_EmptyDenylist(t *testing.T) {
	dl, _ := CompileDenylist(DenylistFile{})

	matched, _ := dl.Denies("shell", shellInput("rm -rf /"))
	if matched {
		t.Error("expected empty denylist to deny nothing")
	}
}

func TestCompileDenylist_BadRegexSkipped(t *testing.T) {
	dl, err := CompileDenylist(DenylistFile{Shell: []string{"regex:[invalid", "rm -rf /"}})
	if err != nil {
		t.Fatalf("CompileDenylist should not error on bad regex: %v", err)
	}

	// Bad regex is skipped; substring entry still works.
	matched, _ := dl.Denies("shell", shellInput("rm -rf /"))
	if !matched {
		t.Error("expected 'rm -rf /' to still be denied after bad regex is skipped")
	}
}

func TestCompileDenylist_InvalidJSON_NoDeny(t *testing.T) {
	dl, _ := CompileDenylist(DenylistFile{Shell: []string{"rm -rf /"}})

	matched, _ := dl.Denies("shell", []byte("not json"))
	if matched {
		t.Error("expected invalid JSON input to not match denylist")
	}
}
