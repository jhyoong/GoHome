package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Task 2.1: Endpoint + Settings structs
func TestSettings_ParseEndpoint(t *testing.T) {
	raw := `{"endpoints":{"e1":{"wire":"anthropic","baseURL":"http://x","apiKeyEnv":"K","defaultModel":"m","contextWindow":200000}},"defaultEndpoint":"e1"}`

	var s Settings
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	e, ok := s.Endpoints["e1"]
	if !ok {
		t.Fatal("expected endpoint 'e1' to exist")
	}
	if e.Wire != WireAnthropic {
		t.Errorf("Wire: got %q, want %q", e.Wire, WireAnthropic)
	}
	if e.ContextWindow != 200000 {
		t.Errorf("ContextWindow: got %d, want 200000", e.ContextWindow)
	}
	if s.DefaultEndpoint != "e1" {
		t.Errorf("DefaultEndpoint: got %q, want %q", s.DefaultEndpoint, "e1")
	}
}

// Task 2.2: Load + merge global and project settings
func writeJSON(t *testing.T, dir, name string, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func TestLoad_MergesGlobalAndProject(t *testing.T) {
	dir := t.TempDir()

	global := Settings{
		Endpoints: map[string]Endpoint{
			"shared":      {Wire: WireAnthropic, BaseURL: "http://global", DefaultModel: "g"},
			"only-global": {Wire: WireOpenAI, BaseURL: "http://og", DefaultModel: "og"},
		},
		DefaultEndpoint: "shared",
	}
	project := Settings{
		Endpoints: map[string]Endpoint{
			"shared":       {Wire: WireOpenAI, BaseURL: "http://project", DefaultModel: "p"},
			"only-project": {Wire: WireAnthropic, BaseURL: "http://op", DefaultModel: "op"},
		},
		DefaultEndpoint: "shared",
	}

	gPath := writeJSON(t, dir, "global.json", global)
	pPath := writeJSON(t, dir, "project.json", project)

	merged, err := Load(gPath, pPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Project overrides shared key
	if merged.Endpoints["shared"].Wire != WireOpenAI {
		t.Errorf("shared.Wire: got %q, want %q", merged.Endpoints["shared"].Wire, WireOpenAI)
	}
	if merged.Endpoints["shared"].BaseURL != "http://project" {
		t.Errorf("shared.BaseURL: got %q, want http://project", merged.Endpoints["shared"].BaseURL)
	}

	// Global-only key preserved
	if _, ok := merged.Endpoints["only-global"]; !ok {
		t.Error("expected only-global endpoint to be present")
	}

	// Project-only key present
	if _, ok := merged.Endpoints["only-project"]; !ok {
		t.Error("expected only-project endpoint to be present")
	}

	// Project defaultEndpoint wins
	if merged.DefaultEndpoint != "shared" {
		t.Errorf("DefaultEndpoint: got %q, want shared", merged.DefaultEndpoint)
	}
}

func TestLoad_ProjectDefaultEndpointWins(t *testing.T) {
	dir := t.TempDir()

	global := Settings{DefaultEndpoint: "global-default"}
	project := Settings{DefaultEndpoint: "project-default"}

	gPath := writeJSON(t, dir, "global.json", global)
	pPath := writeJSON(t, dir, "project.json", project)

	merged, err := Load(gPath, pPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if merged.DefaultEndpoint != "project-default" {
		t.Errorf("DefaultEndpoint: got %q, want project-default", merged.DefaultEndpoint)
	}
}

func TestLoad_GlobalDefaultKeptWhenProjectEmpty(t *testing.T) {
	dir := t.TempDir()

	global := Settings{DefaultEndpoint: "global-default"}
	project := Settings{} // no defaultEndpoint

	gPath := writeJSON(t, dir, "global.json", global)
	pPath := writeJSON(t, dir, "project.json", project)

	merged, err := Load(gPath, pPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if merged.DefaultEndpoint != "global-default" {
		t.Errorf("DefaultEndpoint: got %q, want global-default", merged.DefaultEndpoint)
	}
}

func TestLoad_MissingFilesNotError(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(filepath.Join(dir, "no-global.json"), filepath.Join(dir, "no-project.json"))
	if err != nil {
		t.Errorf("expected no error for missing files, got: %v", err)
	}
}

func TestLoad_MalformedJSONTreatedAsEmpty(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	global := Settings{DefaultEndpoint: "g"}
	gPath := writeJSON(t, dir, "global.json", global)

	merged, err := Load(gPath, bad)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// malformed project treated as empty; global default kept
	if merged.DefaultEndpoint != "g" {
		t.Errorf("DefaultEndpoint: got %q, want g", merged.DefaultEndpoint)
	}
}
