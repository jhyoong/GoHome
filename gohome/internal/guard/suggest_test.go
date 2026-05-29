package guard

import (
	"encoding/json"
	"testing"
)

func bashInput(cmd string) []byte {
	b, _ := json.Marshal(map[string]string{"command": cmd})
	return b
}

func TestSuggest_NonBash(t *testing.T) {
	// Non-bash tool always returns ""
	if got := Suggest("read", nil); got != "" {
		t.Errorf("Suggest(read) = %q, want %q", got, "")
	}
	if got := Suggest("write", bashInput("anything")); got != "" {
		t.Errorf("Suggest(write) = %q, want %q", got, "")
	}
}

func TestSuggest_GitStatus(t *testing.T) {
	got := Suggest("bash", bashInput("git status -sb"))
	want := "^git status"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuggest_NpmRunBuild(t *testing.T) {
	// npm + run => take 3 tokens
	got := Suggest("bash", bashInput("npm run build foo"))
	want := "^npm run build"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuggest_NpmInstall(t *testing.T) {
	// npm without "run" => take 2 tokens
	got := Suggest("bash", bashInput("npm install lodash"))
	want := "^npm install"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuggest_LsUnlisted(t *testing.T) {
	// ls is not in the known set => take 1 token only
	got := Suggest("bash", bashInput("ls -la /tmp"))
	want := "^ls"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuggest_PythonModuleRun(t *testing.T) {
	// python -m => take 3 tokens
	got := Suggest("bash", bashInput("python -m pytest tests/"))
	want := "^python -m pytest"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuggest_PythonNoModule(t *testing.T) {
	// python without -m => take 2 tokens
	got := Suggest("bash", bashInput("python script.py arg"))
	want := "^python script.py"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuggest_EmptyCommand(t *testing.T) {
	// Empty or whitespace-only command => ""
	if got := Suggest("bash", bashInput("")); got != "" {
		t.Errorf("empty command: got %q, want %q", got, "")
	}
}

func TestSuggest_PnpmRunDev(t *testing.T) {
	got := Suggest("bash", bashInput("pnpm run dev"))
	want := "^pnpm run dev"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSuggest_GoTest(t *testing.T) {
	got := Suggest("bash", bashInput("go test ./..."))
	want := "^go test"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
