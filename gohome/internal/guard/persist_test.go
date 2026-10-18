package guard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestAddProject_BashPattern(t *testing.T) {
	tmpDir := t.TempDir()
	projPath := tmpDir + "/whitelist.json"

	wl, err := Compile(WhitelistFile{}, WhitelistFile{}, projPath)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	if err := wl.AddProject("shell", "^git status"); err != nil {
		t.Fatalf("AddProject shell: %v", err)
	}

	// In-memory: should now allow "git status"
	input, _ := json.Marshal(map[string]string{"command": "git status"})
	if !wl.Allows("shell", input) {
		t.Error("expected in-memory to allow 'git status' after AddProject")
	}

	// On disk: read back and verify
	data, err := os.ReadFile(projPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var wf WhitelistFile
	if err := json.Unmarshal(data, &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !containsStr(wf.Shell, "^git status") {
		t.Errorf("disk: expected ^git status in shell list, got %v", wf.Shell)
	}
}

func TestAddProject_Tool(t *testing.T) {
	tmpDir := t.TempDir()
	projPath := tmpDir + "/whitelist.json"

	wl, err := Compile(WhitelistFile{}, WhitelistFile{}, projPath)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	if err := wl.AddProject("read", ""); err != nil {
		t.Fatalf("AddProject tool: %v", err)
	}

	// In-memory
	if !wl.Allows("read", nil) {
		t.Error("expected 'read' to be allowed in-memory after AddProject")
	}

	// On disk
	data, err := os.ReadFile(projPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var wf WhitelistFile
	if err := json.Unmarshal(data, &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !containsStr(wf.Tools, "read") {
		t.Errorf("disk: expected 'read' in tools list, got %v", wf.Tools)
	}
}

func TestAddProject_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	projPath := tmpDir + "/whitelist.json"

	wl, err := Compile(WhitelistFile{}, WhitelistFile{}, projPath)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	// Add the same pattern twice.
	_ = wl.AddProject("shell", "^ls")
	_ = wl.AddProject("shell", "^ls")

	data, err := os.ReadFile(projPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var wf WhitelistFile
	if err := json.Unmarshal(data, &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	count := 0
	for _, p := range wf.Shell {
		if p == "^ls" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 occurrence of '^ls', got %d", count)
	}
}

func TestAddProject_MissingFile_Created(t *testing.T) {
	// Project file does not exist yet; AddProject should create it.
	tmpDir := t.TempDir()
	projPath := tmpDir + "/nested/dir/whitelist.json"

	wl, err := Compile(WhitelistFile{}, WhitelistFile{}, projPath)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	if err := wl.AddProject("shell", "^make build"); err != nil {
		t.Fatalf("AddProject with missing file: %v", err)
	}

	if _, err := os.Stat(projPath); err != nil {
		t.Errorf("expected file to be created: %v", err)
	}
}

func TestAddProject_ConcurrentBashPatterns(t *testing.T) {
	// N goroutines each add a distinct shell pattern concurrently.
	// After all complete, every pattern must appear in the file exactly once.
	const N = 20

	tmpDir := t.TempDir()
	projPath := tmpDir + "/whitelist.json"

	wl, err := Compile(WhitelistFile{}, WhitelistFile{}, projPath)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			pat := fmt.Sprintf("^pattern-%d", i)
			if err := wl.AddProject("shell", pat); err != nil {
				t.Errorf("goroutine %d AddProject: %v", i, err)
			}
		}()
	}
	wg.Wait()

	data, err := os.ReadFile(projPath)
	if err != nil {
		t.Fatalf("ReadFile after concurrent writes: %v", err)
	}
	var wf WhitelistFile
	if err := json.Unmarshal(data, &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if len(wf.Shell) != N {
		t.Errorf("expected %d shell patterns on disk, got %d: %v", N, len(wf.Shell), wf.Shell)
	}

	// Verify no duplicates and all patterns present.
	seen := make(map[string]int)
	for _, p := range wf.Shell {
		seen[p]++
	}
	for i := 0; i < N; i++ {
		pat := fmt.Sprintf("^pattern-%d", i)
		if seen[pat] != 1 {
			t.Errorf("pattern %q: expected 1 occurrence, got %d", pat, seen[pat])
		}
	}
}

func TestAddProject_NoPath_InMemoryOnly(t *testing.T) {
	// projectPath="" means no file; AddProject still updates in-memory state.
	wl, err := Compile(WhitelistFile{}, WhitelistFile{}, "")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	if err := wl.AddProject("shell", "^echo"); err != nil {
		t.Fatalf("AddProject with no path: %v", err)
	}

	input, _ := json.Marshal(map[string]string{"command": "echo hello"})
	if !wl.Allows("shell", input) {
		t.Error("expected 'echo hello' to be allowed in-memory when no projectPath set")
	}
}

func TestAddProject_RejectsEmptyAndInvalid(t *testing.T) {
	for _, pat := range []string{"", "   ", "^", "^ ", "^c++"} {
		projPath := t.TempDir() + "/whitelist.json"
		wl, err := Compile(WhitelistFile{}, WhitelistFile{}, projPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := wl.AddProject("shell", pat); err == nil {
			t.Errorf("AddProject(%q): expected error", pat)
		}
		if _, err := os.Stat(projPath); err == nil {
			t.Errorf("AddProject(%q): file was written", pat)
		}
		if wl.Allows("shell", []byte(`{"command":"rm -rf /"}`)) {
			t.Errorf("AddProject(%q): whitelisted everything", pat)
		}
	}
}

func TestAddProject_NoDuplicateForms(t *testing.T) {
	projPath := t.TempDir() + "/whitelist.json"
	wl, err := Compile(WhitelistFile{}, WhitelistFile{}, projPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, pat := range []string{"ls", "^ls", "ls"} {
		if err := wl.AddProject("shell", pat); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(projPath)
	var wf WhitelistFile
	if err := json.Unmarshal(data, &wf); err != nil {
		t.Fatal(err)
	}
	if len(wf.Shell) != 1 || wf.Shell[0] != "^ls" {
		t.Errorf("shell = %v, want [^ls]", wf.Shell)
	}
	if len(wl.shell) != 1 {
		t.Errorf("in-memory patterns = %d, want 1", len(wl.shell))
	}
}

func TestAddProject_DedupAgainstLegacyUnanchoredEntry(t *testing.T) {
	projPath := t.TempDir() + "/whitelist.json"
	if err := os.WriteFile(projPath, []byte(`{"shell":["ls"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	wl, err := Compile(WhitelistFile{}, WhitelistFile{Shell: []string{"ls"}}, projPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := wl.AddProject("shell", "^ls"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(projPath)
	var wf WhitelistFile
	_ = json.Unmarshal(data, &wf)
	if len(wf.Shell) != 1 {
		t.Errorf("shell = %v, want one entry", wf.Shell)
	}
	if len(wl.shell) != 1 {
		t.Errorf("in-memory patterns = %d, want 1", len(wl.shell))
	}
}

func TestAlternationStaysAnchored(t *testing.T) {
	wl, err := Compile(WhitelistFile{}, WhitelistFile{Shell: []string{"ls|rm"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := wl.AddProject("shell", "^git status|git diff"); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"cat x --norm", "chmod 777 /etc/passwd --norm", "echo git diff", "ls-evil"} {
		if wl.Allows("shell", bashInput(cmd)) {
			t.Errorf("%q should not be allowed", cmd)
		}
	}
	for _, cmd := range []string{"ls -la", "rm -rf x", "git diff", "git status"} {
		if !wl.Allows("shell", bashInput(cmd)) {
			t.Errorf("%q should be allowed", cmd)
		}
	}
}

// A new project whitelist file and its folder are private to the user.
func TestAddProject_NewFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not enforced on Windows")
	}
	dir := filepath.Join(t.TempDir(), ".gohome")
	projPath := filepath.Join(dir, "whitelist.json")
	wl, _ := Compile(WhitelistFile{}, WhitelistFile{}, projPath)
	if err := wl.AddProject("shell", "^ls"); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	for path, want := range map[string]os.FileMode{projPath: 0o600, dir: 0o700} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat %s: %v", path, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", path, got, want)
		}
	}
}
