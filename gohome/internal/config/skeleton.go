package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// SkeletonJSON returns a template settings.json with all fields shown.
func SkeletonJSON() []byte {
	return []byte(`{
  "modelConfig": {
    "example": {
      "wire": "string",
      "baseURL": "string",
      "apiKey": "string",
      "apiKeyEnv": "string",
      "modelName": "string",
      "contextWindow": 0,
      "maxTokens": 0,
      "thinkingBudget": 0,
      "reasoningEffort": "string",
      "headers": {}
    }
  },
  "defaultModel": "string",
  "systemPrompt": "string",
  "bashTimeoutMs": 0,
  "maxBashTimeoutMs": 0,
  "contextWarnPct": 0.0,
  "contextCritPct": 0.0,
  "retryBackoffMs": [],
  "renderThrottleMs": 0
}
`)
}

// ResolveEditor returns the user's preferred editor from VISUAL or EDITOR,
// falling back to "vi" (or "notepad" on Windows).
func ResolveEditor() string {
	if v := os.Getenv("VISUAL"); v != "" {
		return v
	}
	if v := os.Getenv("EDITOR"); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	return "vi"
}

// EnsureConfigFile creates a settings file with skeleton content if it does
// not already exist. Parent directories are created as needed.
func EnsureConfigFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, SkeletonJSON(), 0o644)
}
