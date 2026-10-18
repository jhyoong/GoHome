package guard

import (
	"encoding/json"
	"regexp"
	"testing"
)

func bashInput(cmd string) []byte {
	b, _ := json.Marshal(map[string]string{"command": cmd})
	return b
}

func TestSuggest_NonBash(t *testing.T) {
	// Non-shell tool always returns ""
	if got := Suggest("read", nil); got != "" {
		t.Errorf("Suggest(read) = %q, want %q", got, "")
	}
	if got := Suggest("write", bashInput("anything")); got != "" {
		t.Errorf("Suggest(write) = %q, want %q", got, "")
	}
}

func TestSuggest_GitStatus(t *testing.T) {
	got := Suggest("shell", bashInput("git status -sb"))
	want := "^git status"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuggest_NpmRunBuild(t *testing.T) {
	// npm + run => take 3 tokens
	got := Suggest("shell", bashInput("npm run build foo"))
	want := "^npm run build"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuggest_NpmInstall(t *testing.T) {
	// npm without "run" => take 2 tokens
	got := Suggest("shell", bashInput("npm install lodash"))
	want := "^npm install"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuggest_LsUnlisted(t *testing.T) {
	// ls is not in the known set => take 1 token only
	got := Suggest("shell", bashInput("ls -la /tmp"))
	want := "^ls"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuggest_PythonModuleRun(t *testing.T) {
	// python -m => take 3 tokens
	got := Suggest("shell", bashInput("python -m pytest tests/"))
	want := "^python -m pytest"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuggest_PythonNoModule(t *testing.T) {
	// python without -m => take 2 tokens
	got := Suggest("shell", bashInput("python script.py arg"))
	want := `^python script\.py`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuggest_EmptyCommand(t *testing.T) {
	// Empty or whitespace-only command => ""
	if got := Suggest("shell", bashInput("")); got != "" {
		t.Errorf("empty command: got %q, want %q", got, "")
	}
}

func TestSuggest_PnpmRunDev(t *testing.T) {
	got := Suggest("shell", bashInput("pnpm run dev"))
	want := "^pnpm run dev"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuggest_GoTest(t *testing.T) {
	got := Suggest("shell", bashInput("go test ./..."))
	want := "^go test"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuggest_SudoIsFullCommand(t *testing.T) {
	got := Suggest("shell", bashInput("sudo apt install x"))
	want := "^sudo apt install x$"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuggest_EscapesSpecialChars(t *testing.T) {
	got := Suggest("shell", bashInput("./build.sh"))
	re := regexp.MustCompile(got)
	if re.MatchString("./buildXsh") {
		t.Errorf("suggestion %q is not escaped", got)
	}
	if _, err := regexp.Compile(Suggest("shell", bashInput("c++ main.cpp"))); err != nil {
		t.Errorf("c++ suggestion is not a valid regex: %v", err)
	}
}

func TestSuggest_AllowsOriginalCommand(t *testing.T) {
	for _, cmd := range []string{"./build.sh", "c++ main.cpp", "sudo apt install x", "git status -sb"} {
		wl, err := Compile(WhitelistFile{}, WhitelistFile{}, t.TempDir()+"/w.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := wl.AddProject("shell", Suggest("shell", bashInput(cmd))); err != nil {
			t.Fatalf("%q: AddProject: %v", cmd, err)
		}
		if !wl.Allows("shell", bashInput(cmd)) {
			t.Errorf("%q: not allowed after saving its suggestion", cmd)
		}
	}
	// A different sudo command must not be covered.
	wl, _ := Compile(WhitelistFile{}, WhitelistFile{}, t.TempDir()+"/w.json")
	_ = wl.AddProject("shell", Suggest("shell", bashInput("sudo apt install x")))
	if wl.Allows("shell", bashInput("sudo rm -rf /")) {
		t.Error("sudo suggestion whitelisted other sudo commands")
	}
	// The escaped build.sh rule must not cover build.shred.
	wl, _ = Compile(WhitelistFile{}, WhitelistFile{}, t.TempDir()+"/w.json")
	_ = wl.AddProject("shell", Suggest("shell", bashInput("./build.sh")))
	if wl.Allows("shell", bashInput("./build.shred")) || wl.Allows("shell", bashInput("./buildXsh")) {
		t.Error("escaped suggestion matched a different command")
	}
}

func TestSuggest_SudoDoesNotCoverExtraArgs(t *testing.T) {
	wl, _ := Compile(WhitelistFile{}, WhitelistFile{}, t.TempDir()+"/w.json")
	if err := wl.AddProject("shell", Suggest("shell", bashInput("sudo chmod 644 f"))); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if !wl.Allows("shell", bashInput("sudo chmod 644 f")) {
		t.Error("same sudo command not allowed")
	}
	if wl.Allows("shell", bashInput("sudo chmod 644 f /etc/shadow")) {
		t.Error("sudo suggestion allowed an extra argument")
	}
}

func TestSuggest_ControlCharsGiveNoSuggestion(t *testing.T) {
	for _, cmd := range []string{"\x1b.\x1b* foo", "ls\rfoo", "ls \x7f", "sudo ls\x07", "ls \u009b"} {
		if got := Suggest("shell", bashInput(cmd)); got != "" {
			t.Errorf("%q: got %q, want no suggestion", cmd, got)
		}
	}
	// Tab is not a control character for this rule.
	if got := Suggest("shell", bashInput("ls\t-la")); got != "^ls" {
		t.Errorf("tab command: got %q, want %q", got, "^ls")
	}
}

// Bidi characters can make a command look different from what runs, so a
// command with one gets no suggested pattern.
func TestSuggest_BidiGivesNoSuggestion(t *testing.T) {
	for _, cmd := range []string{"ls # \u202etxt.exe", "ls \u2066x", "ls \u200f", "ls \u061c"} {
		if got := Suggest("shell", bashInput(cmd)); got != "" {
			t.Errorf("%q: got %q, want no suggestion", cmd, got)
		}
	}
}

// Chained commands are never auto-approved, so a suggested pattern for one
// would never match. They get no suggestion, sudo included.
func TestSuggest_ShellOperatorGivesNoSuggestion(t *testing.T) {
	for _, cmd := range []string{"git status; echo x", "ls | wc", "echo $(id)", "ls > out", "sudo ls; id", "sudo ls && id"} {
		if got := Suggest("shell", bashInput(cmd)); got != "" {
			t.Errorf("%q: got %q, want no suggestion", cmd, got)
		}
	}
}
