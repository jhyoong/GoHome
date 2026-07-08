package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/config"
)

func sampleAnnotated() config.AnnotatedSettings {
	return config.AnnotatedSettings{
		Settings: config.Settings{
			DefaultModel:  "my-model",
			BashTimeoutMs: 60000,
			ModelConfig: map[string]config.ModelConfig{
				"my-model": {
					Wire:      config.WireAnthropic,
					BaseURL:   "https://api.anthropic.com",
					ModelName: "claude-sonnet-4-20250514",
				},
			},
		},
		GlobalPath:  "~/.gohome/settings.json",
		ProjectPath: "./.gohome/settings.json",
		Sources: map[string]config.Source{
			"defaultModel":     config.SourceGlobal,
			"bashTimeoutMs":    config.SourceGlobal,
			"maxBashTimeoutMs": config.SourceDefault,
			"contextWarnPct":   config.SourceDefault,
			"contextCritPct":   config.SourceDefault,
			"systemPrompt":     config.SourceDefault,
			"retryBackoffMs":   config.SourceDefault,
			"renderThrottleMs": config.SourceDefault,
		},
		ModelSources: map[string]config.Source{
			"my-model": config.SourceGlobal,
		},
	}
}

func TestConfigOverlay_Render_ShowsSettings(t *testing.T) {
	co := NewConfigOverlay(sampleAnnotated(), func() {}, nil)
	lines := co.Render(80)
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "my-model") {
		t.Error("expected model name in render output")
	}
	if !strings.Contains(joined, "60000") {
		t.Error("expected bashTimeoutMs value in render output")
	}
	if !strings.Contains(joined, "global") {
		t.Error("expected source annotation in render output")
	}
}

func TestConfigOverlay_Render_ShowsActionItems(t *testing.T) {
	co := NewConfigOverlay(sampleAnnotated(), func() {}, nil)
	lines := co.Render(80)
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "Edit global config") {
		t.Error("expected 'Edit global config' action item")
	}
	if !strings.Contains(joined, "Edit project config") {
		t.Error("expected 'Edit project config' action item")
	}
	if !strings.Contains(joined, "Run setup wizard") {
		t.Error("expected 'Run setup wizard' action item")
	}
}

func TestConfigOverlay_EscCloses(t *testing.T) {
	closed := false
	co := NewConfigOverlay(sampleAnnotated(), func() { closed = true }, nil)
	co.HandleInput(tea.KeyMsg{Type: tea.KeyEsc})
	if !closed {
		t.Fatal("expected Esc to trigger close callback")
	}
}

func TestConfigOverlay_ArrowNavigation(t *testing.T) {
	co := NewConfigOverlay(sampleAnnotated(), func() {}, nil)
	if co.actions.selected != 0 {
		t.Fatalf("initial selection: got %d, want 0", co.actions.selected)
	}
	co.HandleInput(tea.KeyMsg{Type: tea.KeyDown})
	if co.actions.selected != 1 {
		t.Errorf("after down: got %d, want 1", co.actions.selected)
	}
	co.HandleInput(tea.KeyMsg{Type: tea.KeyUp})
	if co.actions.selected != 0 {
		t.Errorf("after up: got %d, want 0", co.actions.selected)
	}
}

func TestConfigOverlay_EnterSelectsAction(t *testing.T) {
	var selectedAction string
	onEdit := func(scope string) {
		selectedAction = scope
	}
	co := NewConfigOverlay(sampleAnnotated(), func() {}, onEdit)
	// First item is "Edit global config"
	co.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})
	if selectedAction != "global" {
		t.Errorf("expected global action, got %q", selectedAction)
	}
}

func TestConfigOverlay_Render_EmptyConfig(t *testing.T) {
	empty := config.AnnotatedSettings{
		Settings: config.Settings{
			ModelConfig: map[string]config.ModelConfig{},
		},
		Sources:      map[string]config.Source{},
		ModelSources: map[string]config.Source{},
	}
	co := NewConfigOverlay(empty, func() {}, nil)
	lines := co.Render(80)
	if len(lines) == 0 {
		t.Fatal("expected non-empty render even with empty config")
	}
}
