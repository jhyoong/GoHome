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
	want := "^sudo apt install x"
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
