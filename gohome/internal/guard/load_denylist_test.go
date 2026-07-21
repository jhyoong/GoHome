package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeDenylistFile(t *testing.T, path string, df DenylistFile) {
	t.Helper()
	data, err := json.Marshal(df)
	if err != nil {
		t.Fatalf("writeDenylistFile: marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writeDenylistFile: write: %v", err)
	}
}

func TestLoadDenylist_BothMissing_UsesDefaults(t *testing.T) {
	dir := t.TempDir()
	dl, err := LoadDenylist(
		filepath.Join(dir, "global", "denylist.json"),
		filepath.Join(dir, "project", "denylist.json"),
	)
	if err != nil {
		t.Fatalf("LoadDenylist: %v", err)
	}

	// Default patterns should block "rm -rf /"
	matched, _ := dl.Denies("shell", shellInput("rm -rf /"))
	if !matched {
		t.Error("expected defaults to deny 'rm -rf /'")
	}
}

func TestLoadDenylist_GlobalExists_NoDefaults(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "denylist.json")
	writeDenylistFile(t, globalPath, DenylistFile{Shell: []string{"custom-danger"}})

	dl, err := LoadDenylist(globalPath, filepath.Join(dir, "missing.json"))
	if err != nil {
		t.Fatalf("LoadDenylist: %v", err)
	}

	// User pattern works.
	matched, _ := dl.Denies("shell", shellInput("run custom-danger now"))
	if !matched {
		t.Error("expected user pattern to deny")
	}

	// Default pattern should NOT be active (user file replaces defaults).
	matched, _ = dl.Denies("shell", shellInput("mkfs"))
	if matched {
		t.Error("expected defaults to be replaced when user file exists")
	}
}

func TestLoadDenylist_ProjectExists_NoDefaults(t *testing.T) {
	dir := t.TempDir()
	projectPath := filepath.Join(dir, "denylist.json")
	writeDenylistFile(t, projectPath, DenylistFile{Shell: []string{"project-danger"}})

	dl, err := LoadDenylist(filepath.Join(dir, "missing.json"), projectPath)
	if err != nil {
		t.Fatalf("LoadDenylist: %v", err)
	}

	matched, _ := dl.Denies("shell", shellInput("project-danger"))
	if !matched {
		t.Error("expected project pattern to deny")
	}

	// Defaults not active.
	matched, _ = dl.Denies("shell", shellInput("rm -rf /"))
	if matched {
		t.Error("expected defaults to be replaced when project file exists")
	}
}

func TestLoadDenylist_BothExist_Merged(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "global.json")
	projectPath := filepath.Join(dir, "project.json")

	writeDenylistFile(t, globalPath, DenylistFile{Shell: []string{"global-bad"}})
	writeDenylistFile(t, projectPath, DenylistFile{Shell: []string{"project-bad"}})

	dl, err := LoadDenylist(globalPath, projectPath)
	if err != nil {
		t.Fatalf("LoadDenylist: %v", err)
	}

	// Both patterns active (union).
	matched, _ := dl.Denies("shell", shellInput("global-bad"))
	if !matched {
		t.Error("expected global pattern to deny")
	}
	matched, _ = dl.Denies("shell", shellInput("project-bad"))
	if !matched {
		t.Error("expected project pattern to deny")
	}
}

func TestLoadDenylist_MalformedJSON_TreatedAsEmpty(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "denylist.json")
	if err := os.WriteFile(globalPath, []byte("{bad json"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Malformed file treated as absent; defaults should apply.
	dl, err := LoadDenylist(globalPath, filepath.Join(dir, "missing.json"))
	if err != nil {
		t.Fatalf("LoadDenylist: %v", err)
	}

	matched, _ := dl.Denies("shell", shellInput("rm -rf /"))
	if !matched {
		t.Error("expected defaults when file is malformed")
	}
}
