package config

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
)

// ErrNoAPIKey is returned when a model config has no usable API key.
var ErrNoAPIKey = errors.New("no API key configured")

// Wire identifies which LLM HTTP protocol a model config speaks.
type Wire string

const (
	WireAnthropic Wire = "anthropic"
	WireOpenAI    Wire = "openai"
)

// ModelConfig holds connection details for a single LLM model config.
type ModelConfig struct {
	Wire            Wire              `json:"wire"`
	BaseURL         string            `json:"baseURL"`
	APIKey          string            `json:"apiKey,omitempty"`
	APIKeyEnv       string            `json:"apiKeyEnv,omitempty"`
	ModelName       string            `json:"modelName"`
	ContextWindow   int               `json:"contextWindow,omitempty"`
	MaxTokens       int               `json:"maxTokens,omitempty"`
	ThinkingBudget  int               `json:"thinkingBudget,omitempty"`
	ReasoningEffort string            `json:"reasoningEffort,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
}

// Settings is the top-level configuration structure.
type Settings struct {
	ModelConfig       map[string]ModelConfig `json:"modelConfig"`
	DefaultModel      string                 `json:"defaultModel"`
	SystemPrompt      string                 `json:"systemPrompt,omitempty"`
	ShellTimeoutMs    int                    `json:"shellTimeoutMs,omitempty"`
	MaxShellTimeoutMs int                    `json:"maxShellTimeoutMs,omitempty"`
	ContextWarnPct    float64                `json:"contextWarnPct,omitempty"`
	ContextCritPct    float64                `json:"contextCritPct,omitempty"`
	RetryBackoffMs    []int                  `json:"retryBackoffMs,omitempty"`
	RenderThrottleMs  int                    `json:"renderThrottleMs,omitempty"`
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
		ModelConfig:       make(map[string]ModelConfig),
		DefaultModel:      global.DefaultModel,
		ShellTimeoutMs:    global.ShellTimeoutMs,
		MaxShellTimeoutMs: global.MaxShellTimeoutMs,
		ContextWarnPct:    global.ContextWarnPct,
		ContextCritPct:    global.ContextCritPct,
		RetryBackoffMs:    global.RetryBackoffMs,
		RenderThrottleMs:  global.RenderThrottleMs,
	}

	for k, v := range global.ModelConfig {
		merged.ModelConfig[k] = v
	}
	for k, v := range project.ModelConfig {
		merged.ModelConfig[k] = v
	}

	if project.DefaultModel != "" {
		merged.DefaultModel = project.DefaultModel
	}
	if global.SystemPrompt != "" {
		merged.SystemPrompt = global.SystemPrompt
	}
	if project.SystemPrompt != "" {
		merged.SystemPrompt = project.SystemPrompt
	}
	if project.ShellTimeoutMs != 0 {
		merged.ShellTimeoutMs = project.ShellTimeoutMs
	}
	if project.MaxShellTimeoutMs != 0 {
		merged.MaxShellTimeoutMs = project.MaxShellTimeoutMs
	}
	if project.ContextWarnPct != 0 {
		merged.ContextWarnPct = project.ContextWarnPct
	}
	if project.ContextCritPct != 0 {
		merged.ContextCritPct = project.ContextCritPct
	}
	if len(project.RetryBackoffMs) > 0 {
		merged.RetryBackoffMs = project.RetryBackoffMs
	}
	if project.RenderThrottleMs != 0 {
		merged.RenderThrottleMs = project.RenderThrottleMs
	}

	return merged, nil
}

// ResolveAPIKey returns the API key for the given model config.
// A literal APIKey field takes precedence over APIKeyEnv.
// Returns ErrNoAPIKey when neither is configured.
func ResolveAPIKey(e ModelConfig) (string, error) {
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

// Source identifies where a setting value came from.
type Source string

const (
	SourceGlobal  Source = "global"
	SourceProject Source = "project"
	SourceDefault Source = "default"
)

// AnnotatedSettings wraps merged Settings with provenance info for each field.
type AnnotatedSettings struct {
	Settings
	GlobalPath   string
	ProjectPath  string
	Sources      map[string]Source
	ModelSources map[string]Source
}

// LoadAnnotated loads global and project settings, merges them, and returns
// an AnnotatedSettings that records which source provided each value.
func LoadAnnotated(globalPath, projectPath string) (AnnotatedSettings, error) {
	global := load(globalPath)
	project := load(projectPath)

	merged, err := Load(globalPath, projectPath)
	if err != nil {
		return AnnotatedSettings{}, err
	}

	sources := map[string]Source{
		"defaultModel":      SourceDefault,
		"systemPrompt":      SourceDefault,
		"shellTimeoutMs":    SourceDefault,
		"maxShellTimeoutMs": SourceDefault,
		"contextWarnPct":    SourceDefault,
		"contextCritPct":    SourceDefault,
		"retryBackoffMs":    SourceDefault,
		"renderThrottleMs":  SourceDefault,
	}

	if global.DefaultModel != "" {
		sources["defaultModel"] = SourceGlobal
	}
	if project.DefaultModel != "" {
		sources["defaultModel"] = SourceProject
	}

	if global.SystemPrompt != "" {
		sources["systemPrompt"] = SourceGlobal
	}
	if project.SystemPrompt != "" {
		sources["systemPrompt"] = SourceProject
	}

	if global.ShellTimeoutMs != 0 {
		sources["shellTimeoutMs"] = SourceGlobal
	}
	if project.ShellTimeoutMs != 0 {
		sources["shellTimeoutMs"] = SourceProject
	}

	if global.MaxShellTimeoutMs != 0 {
		sources["maxShellTimeoutMs"] = SourceGlobal
	}
	if project.MaxShellTimeoutMs != 0 {
		sources["maxShellTimeoutMs"] = SourceProject
	}

	if global.ContextWarnPct != 0 {
		sources["contextWarnPct"] = SourceGlobal
	}
	if project.ContextWarnPct != 0 {
		sources["contextWarnPct"] = SourceProject
	}

	if global.ContextCritPct != 0 {
		sources["contextCritPct"] = SourceGlobal
	}
	if project.ContextCritPct != 0 {
		sources["contextCritPct"] = SourceProject
	}

	if len(global.RetryBackoffMs) > 0 {
		sources["retryBackoffMs"] = SourceGlobal
	}
	if len(project.RetryBackoffMs) > 0 {
		sources["retryBackoffMs"] = SourceProject
	}

	if global.RenderThrottleMs != 0 {
		sources["renderThrottleMs"] = SourceGlobal
	}
	if project.RenderThrottleMs != 0 {
		sources["renderThrottleMs"] = SourceProject
	}

	modelSources := make(map[string]Source)
	for k := range global.ModelConfig {
		modelSources[k] = SourceGlobal
	}
	for k := range project.ModelConfig {
		modelSources[k] = SourceProject
	}

	return AnnotatedSettings{
		Settings:     merged,
		GlobalPath:   globalPath,
		ProjectPath:  projectPath,
		Sources:      sources,
		ModelSources: modelSources,
	}, nil
}
