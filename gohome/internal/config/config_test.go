package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Task 2.1: ModelConfig + Settings structs
func TestSettings_ParseModelConfig(t *testing.T) {
	raw := `{"modelConfig":{"e1":{"wire":"anthropic","baseURL":"http://x","apiKeyEnv":"K","modelName":"m","contextWindow":200000}},"defaultModel":"e1"}`

	var s Settings
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	e, ok := s.ModelConfig["e1"]
	if !ok {
		t.Fatal("expected model config 'e1' to exist")
	}
	if e.Wire != WireAnthropic {
		t.Errorf("Wire: got %q, want %q", e.Wire, WireAnthropic)
	}
	if e.ContextWindow != 200000 {
		t.Errorf("ContextWindow: got %d, want 200000", e.ContextWindow)
	}
	if s.DefaultModel != "e1" {
		t.Errorf("DefaultModel: got %q, want %q", s.DefaultModel, "e1")
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
		ModelConfig: map[string]ModelConfig{
			"shared":      {Wire: WireAnthropic, BaseURL: "http://global", ModelName: "g"},
			"only-global": {Wire: WireOpenAI, BaseURL: "http://og", ModelName: "og"},
		},
		DefaultModel: "shared",
	}
	project := Settings{
		ModelConfig: map[string]ModelConfig{
			"shared":       {Wire: WireOpenAI, BaseURL: "http://project", ModelName: "p"},
			"only-project": {Wire: WireAnthropic, BaseURL: "http://op", ModelName: "op"},
		},
		DefaultModel: "shared",
	}

	gPath := writeJSON(t, dir, "global.json", global)
	pPath := writeJSON(t, dir, "project.json", project)

	merged, err := Load(gPath, pPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Project overrides shared key
	if merged.ModelConfig["shared"].Wire != WireOpenAI {
		t.Errorf("shared.Wire: got %q, want %q", merged.ModelConfig["shared"].Wire, WireOpenAI)
	}
	if merged.ModelConfig["shared"].BaseURL != "http://project" {
		t.Errorf("shared.BaseURL: got %q, want http://project", merged.ModelConfig["shared"].BaseURL)
	}

	// Global-only key preserved
	if _, ok := merged.ModelConfig["only-global"]; !ok {
		t.Error("expected only-global model config to be present")
	}

	// Project-only key present
	if _, ok := merged.ModelConfig["only-project"]; !ok {
		t.Error("expected only-project model config to be present")
	}

	// Project defaultModel wins
	if merged.DefaultModel != "shared" {
		t.Errorf("DefaultModel: got %q, want shared", merged.DefaultModel)
	}
}

func TestLoad_ProjectDefaultModelWins(t *testing.T) {
	dir := t.TempDir()

	global := Settings{DefaultModel: "global-default"}
	project := Settings{DefaultModel: "project-default"}

	gPath := writeJSON(t, dir, "global.json", global)
	pPath := writeJSON(t, dir, "project.json", project)

	merged, err := Load(gPath, pPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if merged.DefaultModel != "project-default" {
		t.Errorf("DefaultModel: got %q, want project-default", merged.DefaultModel)
	}
}

func TestLoad_GlobalDefaultKeptWhenProjectEmpty(t *testing.T) {
	dir := t.TempDir()

	global := Settings{DefaultModel: "global-default"}
	project := Settings{} // no defaultModel

	gPath := writeJSON(t, dir, "global.json", global)
	pPath := writeJSON(t, dir, "project.json", project)

	merged, err := Load(gPath, pPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if merged.DefaultModel != "global-default" {
		t.Errorf("DefaultModel: got %q, want global-default", merged.DefaultModel)
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
	global := Settings{DefaultModel: "g"}
	gPath := writeJSON(t, dir, "global.json", global)

	merged, err := Load(gPath, bad)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// malformed project treated as empty; global default kept
	if merged.DefaultModel != "g" {
		t.Errorf("DefaultModel: got %q, want g", merged.DefaultModel)
	}
}

// Task 2.3: API key resolution
func TestResolveAPIKey_LiteralKey(t *testing.T) {
	key, err := ResolveAPIKey(ModelConfig{APIKey: "literal-key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "literal-key" {
		t.Errorf("got %q, want literal-key", key)
	}
}

func TestResolveAPIKey_EnvVar(t *testing.T) {
	t.Setenv("TEST_API_KEY_VAR", "env-value")
	key, err := ResolveAPIKey(ModelConfig{APIKeyEnv: "TEST_API_KEY_VAR"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "env-value" {
		t.Errorf("got %q, want env-value", key)
	}
}

func TestResolveAPIKey_LiteralTakesPrecedenceOverEnv(t *testing.T) {
	t.Setenv("TEST_API_KEY_BOTH", "env-value")
	key, err := ResolveAPIKey(ModelConfig{APIKey: "literal", APIKeyEnv: "TEST_API_KEY_BOTH"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "literal" {
		t.Errorf("got %q, want literal", key)
	}
}

func TestResolveAPIKey_NeitherReturnsError(t *testing.T) {
	_, err := ResolveAPIKey(ModelConfig{})
	if !errors.Is(err, ErrNoAPIKey) {
		t.Errorf("got %v, want ErrNoAPIKey", err)
	}
}

// Task 2.4: Default paths
func TestDefaultGlobalPath(t *testing.T) {
	p, err := DefaultGlobalPath()
	if err != nil {
		t.Fatalf("DefaultGlobalPath: %v", err)
	}
	suffix := filepath.Join(".gohome", "settings.json")
	if !strings.HasSuffix(p, suffix) {
		t.Errorf("path %q does not end with %q", p, suffix)
	}
}

func TestDefaultProjectPath(t *testing.T) {
	cwd := t.TempDir()
	p := DefaultProjectPath(cwd)
	suffix := filepath.Join(".gohome", "settings.json")
	if !strings.HasSuffix(p, suffix) {
		t.Errorf("path %q does not end with %q", p, suffix)
	}
	if !strings.HasPrefix(p, cwd) {
		t.Errorf("path %q does not start with cwd %q", p, cwd)
	}
}

// Task: new Settings fields — merge behaviour
func TestLoad_MergesNewSettingsFields(t *testing.T) {
	dir := t.TempDir()

	global := Settings{ShellTimeoutMs: 60000, ContextWarnPct: 0.70}
	project := Settings{ShellTimeoutMs: 90000}

	gPath := writeJSON(t, dir, "global.json", global)
	pPath := writeJSON(t, dir, "project.json", project)

	merged, err := Load(gPath, pPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if merged.ShellTimeoutMs != 90000 {
		t.Errorf("ShellTimeoutMs: got %d, want 90000", merged.ShellTimeoutMs)
	}
	if merged.ContextWarnPct != 0.70 {
		t.Errorf("ContextWarnPct: got %v, want 0.70", merged.ContextWarnPct)
	}
}

func TestLoad_ProjectOverridesRetryBackoff(t *testing.T) {
	dir := t.TempDir()

	global := Settings{RetryBackoffMs: []int{100, 200}}
	project := Settings{RetryBackoffMs: []int{500}}

	gPath := writeJSON(t, dir, "global.json", global)
	pPath := writeJSON(t, dir, "project.json", project)

	merged, err := Load(gPath, pPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(merged.RetryBackoffMs) != 1 || merged.RetryBackoffMs[0] != 500 {
		t.Errorf("RetryBackoffMs: got %v, want [500]", merged.RetryBackoffMs)
	}
}

func TestLoad_ZeroValuesPreserveGlobal(t *testing.T) {
	dir := t.TempDir()

	global := Settings{ContextCritPct: 0.90}
	project := Settings{} // ContextCritPct zero — should not override

	gPath := writeJSON(t, dir, "global.json", global)
	pPath := writeJSON(t, dir, "project.json", project)

	merged, err := Load(gPath, pPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if merged.ContextCritPct != 0.90 {
		t.Errorf("ContextCritPct: got %v, want 0.90", merged.ContextCritPct)
	}
}

func TestLoad_ModelConfigMaxTokensAndThinkingBudget(t *testing.T) {
	dir := t.TempDir()

	global := Settings{
		ModelConfig: map[string]ModelConfig{
			"main": {
				Wire:           WireAnthropic,
				BaseURL:        "http://x",
				ModelName:      "m",
				MaxTokens:      8192,
				ThinkingBudget: 4096,
			},
		},
		DefaultModel: "main",
	}

	gPath := writeJSON(t, dir, "global.json", global)
	pPath := writeJSON(t, dir, "project.json", Settings{})

	merged, err := Load(gPath, pPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ep, ok := merged.ModelConfig["main"]
	if !ok {
		t.Fatal("expected model config 'main' to be present")
	}
	if ep.MaxTokens != 8192 {
		t.Errorf("MaxTokens: got %d, want 8192", ep.MaxTokens)
	}
	if ep.ThinkingBudget != 4096 {
		t.Errorf("ThinkingBudget: got %d, want 4096", ep.ThinkingBudget)
	}
}

func TestLoad_RenderThrottleMsMerge(t *testing.T) {
	dir := t.TempDir()

	global := Settings{RenderThrottleMs: 50}
	project := Settings{RenderThrottleMs: 100}

	gPath := writeJSON(t, dir, "global.json", global)
	pPath := writeJSON(t, dir, "project.json", project)

	merged, err := Load(gPath, pPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if merged.RenderThrottleMs != 100 {
		t.Errorf("RenderThrottleMs: got %d, want 100", merged.RenderThrottleMs)
	}
}

func TestLoad_RenderThrottleMsZeroPreservesGlobal(t *testing.T) {
	dir := t.TempDir()

	global := Settings{RenderThrottleMs: 50}
	project := Settings{}

	gPath := writeJSON(t, dir, "global.json", global)
	pPath := writeJSON(t, dir, "project.json", project)

	merged, err := Load(gPath, pPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if merged.RenderThrottleMs != 50 {
		t.Errorf("RenderThrottleMs: got %d, want 50 (should preserve global)", merged.RenderThrottleMs)
	}
}

func TestLoad_MergesAutoCompactFields(t *testing.T) {
	dir := t.TempDir()

	global := Settings{
		AutoCompact:     true,
		AutoCompactMode: "percentage",
		AutoCompactPct:  0.85,
	}
	project := Settings{
		AutoCompactMode:     "leftover",
		AutoCompactLeftover: 64000,
		AutoCompactPrompt:   "custom prompt",
	}

	gPath := writeJSON(t, dir, "global.json", global)
	pPath := writeJSON(t, dir, "project.json", project)

	merged, err := Load(gPath, pPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !merged.AutoCompact {
		t.Error("AutoCompact: got false, want true")
	}
	if merged.AutoCompactMode != "leftover" {
		t.Errorf("AutoCompactMode: got %q, want leftover", merged.AutoCompactMode)
	}
	if merged.AutoCompactPct != 0.85 {
		t.Errorf("AutoCompactPct: got %v, want 0.85", merged.AutoCompactPct)
	}
	if merged.AutoCompactLeftover != 64000 {
		t.Errorf("AutoCompactLeftover: got %d, want 64000", merged.AutoCompactLeftover)
	}
	if merged.AutoCompactPrompt != "custom prompt" {
		t.Errorf("AutoCompactPrompt: got %q, want 'custom prompt'", merged.AutoCompactPrompt)
	}
}

func TestLoad_AutoCompactZeroPreservesGlobal(t *testing.T) {
	dir := t.TempDir()

	global := Settings{
		AutoCompactPct:       0.90,
		AutoCompactTargetPct: 0.40,
		AutoCompactLeftover:  16000,
	}
	project := Settings{}

	gPath := writeJSON(t, dir, "global.json", global)
	pPath := writeJSON(t, dir, "project.json", project)

	merged, err := Load(gPath, pPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if merged.AutoCompactPct != 0.90 {
		t.Errorf("AutoCompactPct: got %v, want 0.90", merged.AutoCompactPct)
	}
	if merged.AutoCompactTargetPct != 0.40 {
		t.Errorf("AutoCompactTargetPct: got %v, want 0.40", merged.AutoCompactTargetPct)
	}
	if merged.AutoCompactLeftover != 16000 {
		t.Errorf("AutoCompactLeftover: got %d, want 16000", merged.AutoCompactLeftover)
	}
}
