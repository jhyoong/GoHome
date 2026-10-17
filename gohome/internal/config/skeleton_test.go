package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSkeletonJSON_ValidJSON(t *testing.T) {
	data := SkeletonJSON()
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("SkeletonJSON is not valid JSON: %v", err)
	}
}

func TestSkeletonJSON_ContainsAllTopLevelFields(t *testing.T) {
	data := SkeletonJSON()
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	required := []string{
		"modelConfig", "defaultModel", "systemPrompt",
		"shellTimeoutMs", "maxShellTimeoutMs",
		"contextWarnPct", "contextCritPct",
		"retryBackoffMs", "renderThrottleMs",
		"autoCompact", "autoCompactMode",
		"autoCompactPct",
		"autoCompactLeftover", "autoCompactPrompt",
		"cacheSudoPassword",
	}
	for _, key := range required {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing top-level field %q", key)
		}
	}
}

func TestSkeletonJSON_ModelConfigEmpty(t *testing.T) {
	var raw map[string]any
	if err := json.Unmarshal(SkeletonJSON(), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	mc, ok := raw["modelConfig"].(map[string]any)
	if !ok || len(mc) != 0 {
		t.Errorf("modelConfig should be an empty object, got %v", raw["modelConfig"])
	}
}

func TestResolveEditor_EnvEditor(t *testing.T) {
	t.Setenv("EDITOR", "nano")
	t.Setenv("VISUAL", "")
	got := ResolveEditor()
	if got != "nano" {
		t.Errorf("got %q, want nano", got)
	}
}

func TestResolveEditor_EnvVisualTakesPrecedence(t *testing.T) {
	t.Setenv("EDITOR", "nano")
	t.Setenv("VISUAL", "code")
	got := ResolveEditor()
	if got != "code" {
		t.Errorf("got %q, want code", got)
	}
}

func TestResolveEditor_FallbackDefault(t *testing.T) {
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	got := ResolveEditor()
	if runtime.GOOS == "windows" {
		if got != "notepad" {
			t.Errorf("got %q, want notepad on windows", got)
		}
	} else {
		if got != "vi" {
			t.Errorf("got %q, want vi on unix", got)
		}
	}
}

func TestEnsureConfigFile_CreatesWithSkeleton(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sub/settings.json"

	err := EnsureConfigFile(path)
	if err != nil {
		t.Fatalf("EnsureConfigFile: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("written file is not valid JSON: %v", err)
	}
	if _, ok := raw["modelConfig"]; !ok {
		t.Error("written file missing modelConfig")
	}
}

func TestEnsureConfigFile_ExistingFileUntouched(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/settings.json"
	original := []byte(`{"defaultModel":"mine"}`)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	err := EnsureConfigFile(path)
	if err != nil {
		t.Fatalf("EnsureConfigFile: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Errorf("existing file was modified: got %s", data)
	}
}

func TestEnsureConfigFile_OwnerOnlyPerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions")
	}
	path := filepath.Join(t.TempDir(), "newdir", "settings.json")
	if err := EnsureConfigFile(path); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %o, want 600", got)
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := di.Mode().Perm(); got != 0o700 {
		t.Errorf("dir mode = %o, want 700", got)
	}
}

func TestSkeletonJSON_NoPlaceholders(t *testing.T) {
	data := string(SkeletonJSON())
	if strings.Contains(data, `"string"`) || strings.Contains(data, `"example"`) || strings.Contains(data, "autoCompactTargetPct") {
		t.Errorf("skeleton contains placeholder content:\n%s", data)
	}
}
