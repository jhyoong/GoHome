package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jhyoong/gohome/internal/config"
)

func TestParseConfig(t *testing.T) {
	yaml := `
endpoint:
  url: "http://localhost:8080/v1"
  model: "my-model"
  max_tokens: 4096
  temperature: 0.7
server:
  host: "127.0.0.1"
  port: 3000
storage:
  path: "~/.gohome/data.db"
approval:
  default_timeout: 300
  auto_approve_all: false
  whitelist:
    - tool: "file_read"
      allow: "always"
system_prompt: "You are helpful."
`
	f, err := os.CreateTemp("", "config*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(yaml)
	f.Close()

	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Endpoint.URL != "http://localhost:8080/v1" {
		t.Errorf("got URL %q", cfg.Endpoint.URL)
	}
	if cfg.Server.Port != 3000 {
		t.Errorf("got port %d", cfg.Server.Port)
	}
	home, _ := os.UserHomeDir()
	wantPath := filepath.Join(home, ".gohome/data.db")
	if cfg.Storage.Path != wantPath {
		t.Errorf("got path %q, want %q", cfg.Storage.Path, wantPath)
	}
	if len(cfg.Approval.Whitelist) != 1 || cfg.Approval.Whitelist[0].Tool != "file_read" {
		t.Errorf("unexpected whitelist: %+v", cfg.Approval.Whitelist)
	}
}

func TestSaveAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := &config.Config{}
	cfg.Approval.Whitelist = []config.WhitelistEntry{
		{Tool: "file_read", Allow: "always"},
		{Tool: "shell", Allow: "always", CommandPattern: "ls *"},
	}

	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(loaded.Approval.Whitelist) != 2 {
		t.Fatalf("got %d entries, want 2", len(loaded.Approval.Whitelist))
	}
	if loaded.Approval.Whitelist[1].CommandPattern != "ls *" {
		t.Errorf("got pattern %q, want %q", loaded.Approval.Whitelist[1].CommandPattern, "ls *")
	}
}

func TestContextWindowDefault(t *testing.T) {
	yaml := `
endpoint:
  url: "http://localhost:8080/v1"
  model: "my-model"
`
	f, err := os.CreateTemp("", "config*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(yaml)
	f.Close()

	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Endpoint.ContextWindow != 131072 {
		t.Errorf("got ContextWindow %d, want 131072", cfg.Endpoint.ContextWindow)
	}
}

func TestContextWindowExplicit(t *testing.T) {
	yaml := `
endpoint:
  url: "http://localhost:8080/v1"
  model: "my-model"
  context_window: 65536
`
	f, err := os.CreateTemp("", "config*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(yaml)
	f.Close()

	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Endpoint.ContextWindow != 65536 {
		t.Errorf("got ContextWindow %d, want 65536", cfg.Endpoint.ContextWindow)
	}
}

func TestThinkingTokensExplicit(t *testing.T) {
	yaml := `
endpoint:
  url: "http://localhost:8080/v1"
  model: "my-model"
  max_tokens: 4096
  thinking_tokens: 1024
`
	f, err := os.CreateTemp("", "config*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(yaml)
	f.Close()

	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Endpoint.ThinkingTokens != 1024 {
		t.Errorf("got ThinkingTokens %d, want 1024", cfg.Endpoint.ThinkingTokens)
	}
}

func TestThinkingTokensDefault(t *testing.T) {
	yaml := `
endpoint:
  url: "http://localhost:8080/v1"
  model: "my-model"
  max_tokens: 4096
`
	f, err := os.CreateTemp("", "config*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(yaml)
	f.Close()

	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Endpoint.ThinkingTokens != 0 {
		t.Errorf("got ThinkingTokens %d, want 0 (model default)", cfg.Endpoint.ThinkingTokens)
	}
}

func TestThinkingTokensSerialization(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := &config.Config{}
	cfg.Endpoint.URL = "http://localhost:8080/v1"
	cfg.Endpoint.Model = "my-model"
	cfg.Endpoint.MaxTokens = 4096
	cfg.Endpoint.ThinkingTokens = 1024

	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.Endpoint.ThinkingTokens != 1024 {
		t.Errorf("got ThinkingTokens %d, want 1024 after save/reload", loaded.Endpoint.ThinkingTokens)
	}

	if _, err := os.Stat(filepath.Join(dir, "check_yaml.go")); os.IsNotExist(err) {
		// Check that yaml contains thinking_tokens field
		if !contains(string(data), "thinking_tokens") {
			t.Errorf("serialized YAML does not contain 'thinking_tokens' field")
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
