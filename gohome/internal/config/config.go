package config

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
)

// ErrNoAPIKey is returned when an endpoint has no usable API key.
var ErrNoAPIKey = errors.New("no API key configured")

// Wire identifies which LLM HTTP protocol an endpoint speaks.
type Wire string

const (
	WireAnthropic Wire = "anthropic"
	WireOpenAI    Wire = "openai"
)

// Endpoint holds connection details for a single LLM endpoint.
type Endpoint struct {
	Wire          Wire              `json:"wire"`
	BaseURL       string            `json:"baseURL"`
	APIKey        string            `json:"apiKey,omitempty"`
	APIKeyEnv     string            `json:"apiKeyEnv,omitempty"`
	DefaultModel  string            `json:"defaultModel"`
	ContextWindow int               `json:"contextWindow,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
}

// Settings is the top-level configuration structure.
type Settings struct {
	Endpoints       map[string]Endpoint `json:"endpoints"`
	DefaultEndpoint string              `json:"defaultEndpoint"`
}

// load reads and decodes a Settings file at path.
// A missing file returns empty Settings (no error).
// A malformed file logs a warning and returns empty Settings (no error).
func load(path string) Settings {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("config: cannot read file", "path", path, "err", err)
		}
		return Settings{}
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		slog.Warn("config: malformed JSON, treating as empty", "path", path, "err", err)
		return Settings{}
	}
	return s
}

// Load reads globalPath and projectPath, merges them, and returns the result.
// Project settings override global settings. Missing files are silently skipped.
func Load(globalPath, projectPath string) (Settings, error) {
	global := load(globalPath)
	project := load(projectPath)

	merged := Settings{
		Endpoints:       make(map[string]Endpoint),
		DefaultEndpoint: global.DefaultEndpoint,
	}

	for k, v := range global.Endpoints {
		merged.Endpoints[k] = v
	}
	for k, v := range project.Endpoints {
		merged.Endpoints[k] = v
	}

	if project.DefaultEndpoint != "" {
		merged.DefaultEndpoint = project.DefaultEndpoint
	}

	return merged, nil
}

// ResolveAPIKey returns the API key for e.
// A literal APIKey field takes precedence over APIKeyEnv.
// Returns ErrNoAPIKey when neither is configured.
func ResolveAPIKey(e Endpoint) (string, error) {
	if e.APIKey != "" {
		return e.APIKey, nil
	}
	if e.APIKeyEnv != "" {
		val := os.Getenv(e.APIKeyEnv)
		if val != "" {
			return val, nil
		}
	}
	return "", ErrNoAPIKey
}

// DefaultGlobalPath returns the canonical global settings path:
// <home>/.gohome/settings.json
func DefaultGlobalPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gohome", "settings.json"), nil
}

// DefaultProjectPath returns the settings path relative to cwd:
// <cwd>/.gohome/settings.json
func DefaultProjectPath(cwd string) string {
	return filepath.Join(cwd, ".gohome", "settings.json")
}
