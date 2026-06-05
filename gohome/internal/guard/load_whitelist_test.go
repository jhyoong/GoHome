package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeWhitelistFile is a test helper that writes wf as JSON to path.
func writeWhitelistFile(t *testing.T, path string, wf WhitelistFile) {
	t.Helper()
	data, err := json.Marshal(wf)
	if err != nil {
		t.Fatalf("writeWhitelistFile: marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writeWhitelistFile: write: %v", err)
	}
}

func TestLoadWhitelist_BothMissing(t *testing.T) {
	dir := t.TempDir()
	wl, err := LoadWhitelist(
		filepath.Join(dir, "global", "whitelist.json"),
		filepath.Join(dir, "project", "whitelist.json"),
	)
	if err != nil {
		t.Fatalf("LoadWhitelist: %v", err)
	}
	if wl == nil {
		t.Fatal("expected non-nil Whitelist")
	}
	// With both files missing the whitelist allows nothing.
	if wl.Allows("read", nil) {
		t.Error("expected empty whitelist to deny 'read'")
	}
}

func TestLoadWhitelist_GlobalOnly(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "whitelist.json")
	writeWhitelistFile(t, globalPath, WhitelistFile{Tools: []string{"read"}})

	wl, err := LoadWhitelist(globalPath, filepath.Join(dir, "missing.json"))
	if err != nil {
		t.Fatalf("LoadWhitelist: %v", err)
	}
	if !wl.Allows("read", nil) {
		t.Error("expected 'read' to be allowed from global")
	}
	if wl.Allows("write", nil) {
		t.Error("expected 'write' to be denied")
	}
}

func TestLoadWhitelist_ProjectOverridesGlobal(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "global.json")
	projectPath := filepath.Join(dir, "project.json")

	writeWhitelistFile(t, globalPath, WhitelistFile{Tools: []string{"read"}})
	writeWhitelistFile(t, projectPath, WhitelistFile{Tools: []string{"write", "edit"}})

	wl, err := LoadWhitelist(globalPath, projectPath)
	if err != nil {
		t.Fatalf("LoadWhitelist: %v", err)
	}
	// Both global and project entries are present (union).
	for _, tool := range []string{"read", "write", "edit"} {
		if !wl.Allows(tool, nil) {
			t.Errorf("expected %q to be allowed", tool)
		}
	}
}

func TestLoadWhitelist_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "global.json")
	projectPath := filepath.Join(dir, "project.json")

	// Write malformed JSON to global; valid JSON to project.
	if err := os.WriteFile(globalPath, []byte("{bad json"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeWhitelistFile(t, projectPath, WhitelistFile{Tools: []string{"read"}})

	// Should not return an error; malformed file is silently treated as empty.
	wl, err := LoadWhitelist(globalPath, projectPath)
	if err != nil {
		t.Fatalf("LoadWhitelist: expected no error on malformed JSON, got: %v", err)
	}
	// Project entry is still loaded.
	if !wl.Allows("read", nil) {
		t.Error("expected 'read' from project to be allowed despite malformed global")
	}
}

func TestLoadWhitelist_ProjectPathForwarded(t *testing.T) {
	dir := t.TempDir()
	projectPath := filepath.Join(dir, "project.json")
	writeWhitelistFile(t, projectPath, WhitelistFile{})

	wl, err := LoadWhitelist(filepath.Join(dir, "missing_global.json"), projectPath)
	if err != nil {
		t.Fatalf("LoadWhitelist: %v", err)
	}
	// AddProject should persist to the project file (verifies projectPath was forwarded).
	if err := wl.AddProject("write", ""); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if !wl.Allows("write", nil) {
		t.Error("expected 'write' to be allowed after AddProject")
	}
}
