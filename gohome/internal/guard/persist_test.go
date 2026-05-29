package guard

import (
	"encoding/json"
	"fmt"
	"os"
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

	if err := wl.AddProject("bash", "^git status"); err != nil {
		t.Fatalf("AddProject bash: %v", err)
	}

	// In-memory: should now allow "git status"
	input, _ := json.Marshal(map[string]string{"command": "git status"})
	if !wl.Allows("bash", input) {
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
	if !containsStr(wf.Bash, "^git status") {
		t.Errorf("disk: expected ^git status in bash list, got %v", wf.Bash)
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
	_ = wl.AddProject("bash", "^ls")
	_ = wl.AddProject("bash", "^ls")

	data, err := os.ReadFile(projPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var wf WhitelistFile
	if err := json.Unmarshal(data, &wf); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	count := 0
	for _, p := range wf.Bash {
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

	if err := wl.AddProject("bash", "^make build"); err != nil {
		t.Fatalf("AddProject with missing file: %v", err)
	}

	if _, err := os.Stat(projPath); err != nil {
		t.Errorf("expected file to be created: %v", err)
	}
}

func TestAddProject_ConcurrentBashPatterns(t *testing.T) {
	// N goroutines each add a distinct bash pattern concurrently.
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
			if err := wl.AddProject("bash", pat); err != nil {
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

	if len(wf.Bash) != N {
		t.Errorf("expected %d bash patterns on disk, got %d: %v", N, len(wf.Bash), wf.Bash)
	}

	// Verify no duplicates and all patterns present.
	seen := make(map[string]int)
	for _, p := range wf.Bash {
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

	if err := wl.AddProject("bash", "^echo"); err != nil {
		t.Fatalf("AddProject with no path: %v", err)
	}

	input, _ := json.Marshal(map[string]string{"command": "echo hello"})
	if !wl.Allows("bash", input) {
		t.Error("expected 'echo hello' to be allowed in-memory when no projectPath set")
	}
}
