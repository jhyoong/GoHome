package tui

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/config"
)

func TestConfigWizard_InitialStep(t *testing.T) {
	w := NewConfigWizard(func() {}, func(string) {})
	if w.step != wizardStepWire {
		t.Errorf("initial step: got %d, want %d", w.step, wizardStepWire)
	}
}

func TestConfigWizard_Render_ShowsWireOptions(t *testing.T) {
	w := NewConfigWizard(func() {}, func(string) {})
	lines := w.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "anthropic") {
		t.Error("expected 'anthropic' option in wire step")
	}
	if !strings.Contains(joined, "openai") {
		t.Error("expected 'openai' option in wire step")
	}
}

func TestConfigWizard_FullFlow(t *testing.T) {
	dir := t.TempDir()
	outPath := dir + "/settings.json"

	var savedPath string
	w := NewConfigWizard(func() {}, func(path string) { savedPath = path })
	w.outputPath = outPath

	// Step 1: select wire = anthropic (first item, press Enter)
	w.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})
	if w.step != wizardStepBaseURL {
		t.Fatalf("after wire select: step = %d, want %d", w.step, wizardStepBaseURL)
	}

	// Step 2: type base URL + Enter
	for _, r := range "https://api.anthropic.com" {
		w.HandleInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	w.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})
	if w.step != wizardStepKeySource {
		t.Fatalf("after base URL: step = %d, want %d", w.step, wizardStepKeySource)
	}

	// Step 3: select "Environment variable" (first item)
	w.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})
	if w.step != wizardStepKeyValue {
		t.Fatalf("after key source: step = %d, want %d", w.step, wizardStepKeyValue)
	}

	// Step 4: type env var name + Enter
	for _, r := range "ANTHROPIC_API_KEY" {
		w.HandleInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	w.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})
	if w.step != wizardStepModelName {
		t.Fatalf("after key value: step = %d, want %d", w.step, wizardStepModelName)
	}

	// Step 5: type model name + Enter
	for _, r := range "claude-sonnet-4-20250514" {
		w.HandleInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	w.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})
	if w.step != wizardStepConfigName {
		t.Fatalf("after model name: step = %d, want %d", w.step, wizardStepConfigName)
	}

	// Step 6: type config name + Enter
	for _, r := range "claude" {
		w.HandleInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	w.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})
	if w.step != wizardStepSudoCache {
		t.Fatalf("after config name: step = %d, want %d", w.step, wizardStepSudoCache)
	}

	// Step 7: sudo cache -- default "No" (first item)
	w.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})
	if w.step != wizardStepConfirm {
		t.Fatalf("after sudo cache: step = %d, want %d", w.step, wizardStepConfirm)
	}
	if !strings.Contains(w.summaryText(), "Sudo cache:   off") {
		t.Errorf("summary missing default sudo cache line:\n%s", w.summaryText())
	}

	// Step 8: confirm (select "Save", first item)
	w.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})

	// Verify file was written
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var s config.Settings
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.DefaultModel != "claude" {
		t.Errorf("defaultModel: got %q, want claude", s.DefaultModel)
	}
	mc, ok := s.ModelConfig["claude"]
	if !ok {
		t.Fatal("expected model config 'claude' to exist")
	}
	if mc.Wire != config.WireAnthropic {
		t.Errorf("wire: got %q, want anthropic", mc.Wire)
	}
	if mc.APIKeyEnv != "ANTHROPIC_API_KEY" {
		t.Errorf("apiKeyEnv: got %q, want ANTHROPIC_API_KEY", mc.APIKeyEnv)
	}
	if s.CacheSudoPassword {
		t.Error("cacheSudoPassword: got true, want false (default No)")
	}
	if savedPath != outPath {
		t.Errorf("onSave path: got %q, want %q", savedPath, outPath)
	}
}

func TestConfigWizard_SudoCacheYes(t *testing.T) {
	dir := t.TempDir()
	outPath := dir + "/settings.json"
	w := NewConfigWizard(func() {}, func(string) {})
	w.outputPath = outPath
	w.wire, w.baseURL, w.keySource, w.keyValue = "anthropic", "http://x", "env", "K"
	w.modelName, w.configName = "m", "c"
	w.step = wizardStepSudoCache
	w.buildStep()

	if joined := StripAnsi(strings.Join(w.Render(80), "\n")); !strings.Contains(joined, "Cache sudo password") {
		t.Fatalf("missing prompt:\n%s", joined)
	}

	w.HandleInput(tea.KeyMsg{Type: tea.KeyDown})  // move to "Yes"
	w.HandleInput(tea.KeyMsg{Type: tea.KeyEnter}) // select
	if !strings.Contains(w.summaryText(), "Sudo cache:   on") {
		t.Errorf("summary missing sudo cache line:\n%s", w.summaryText())
	}
	w.HandleInput(tea.KeyMsg{Type: tea.KeyEnter}) // Save

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var s config.Settings
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !s.CacheSudoPassword {
		t.Error("cacheSudoPassword: got false, want true")
	}
}

func TestConfigWizard_SudoCacheEscGoesBackToConfigName(t *testing.T) {
	w := NewConfigWizard(func() {}, func(string) {})
	w.outputPath = t.TempDir() + "/settings.json"
	w.step = wizardStepSudoCache
	w.buildStep()
	w.HandleInput(tea.KeyMsg{Type: tea.KeyEsc})
	if w.step != wizardStepConfigName {
		t.Errorf("step = %d, want %d", w.step, wizardStepConfigName)
	}
}

func TestConfigWizard_ConfirmEscGoesBackToSudoCache(t *testing.T) {
	w := NewConfigWizard(func() {}, func(string) {})
	w.outputPath = t.TempDir() + "/settings.json"
	w.step = wizardStepConfirm
	w.buildStep()
	w.HandleInput(tea.KeyMsg{Type: tea.KeyEsc})
	if w.step != wizardStepSudoCache {
		t.Errorf("step = %d, want %d", w.step, wizardStepSudoCache)
	}
}

func TestConfigWizard_EscGoesBack(t *testing.T) {
	w := NewConfigWizard(func() {}, func(string) {})
	// Advance to step 2
	w.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})
	if w.step != wizardStepBaseURL {
		t.Fatalf("expected step %d, got %d", wizardStepBaseURL, w.step)
	}
	// Press Esc to go back
	w.HandleInput(tea.KeyMsg{Type: tea.KeyEsc})
	if w.step != wizardStepWire {
		t.Errorf("expected step %d after Esc, got %d", wizardStepWire, w.step)
	}
}

func TestConfigWizard_EscOnFirstStepCancels(t *testing.T) {
	cancelled := false
	w := NewConfigWizard(func() { cancelled = true }, func(string) {})
	w.HandleInput(tea.KeyMsg{Type: tea.KeyEsc})
	if !cancelled {
		t.Fatal("expected Esc on first step to cancel wizard")
	}
}

func TestConfigWizard_NullBytesStripped(t *testing.T) {
	w := NewConfigWizard(func() {}, func(string) {})
	// Advance past wire selection to a text input step
	w.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})
	if w.step != wizardStepBaseURL {
		t.Fatalf("expected step %d, got %d", wizardStepBaseURL, w.step)
	}
	// Simulate a Windows paste with null bytes interleaved (UTF-16LE artifact)
	w.HandleInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0, 0, 0, 'h', 't', 't', 'p'}})
	if w.textBuf != "http" {
		t.Errorf("expected %q, got %q", "http", w.textBuf)
	}
}

func TestConfigWizard_CancelOnConfirmStep(t *testing.T) {
	cancelled := false
	w := NewConfigWizard(func() { cancelled = true }, func(string) {})
	// Fast-forward to confirm step
	w.step = wizardStepConfirm
	w.rebuildConfirmStep()
	// Select "Cancel" (second item)
	w.HandleInput(tea.KeyMsg{Type: tea.KeyDown})
	w.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})
	if !cancelled {
		t.Fatal("expected Cancel on confirm step to cancel wizard")
	}
}
