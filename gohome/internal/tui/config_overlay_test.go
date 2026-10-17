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
			DefaultModel:   "my-model",
			ShellTimeoutMs: 60000,
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
			"defaultModel":      config.SourceGlobal,
			"shellTimeoutMs":    config.SourceGlobal,
			"maxShellTimeoutMs": config.SourceDefault,
			"contextWarnPct":    config.SourceDefault,
			"contextCritPct":    config.SourceDefault,
			"systemPrompt":      config.SourceDefault,
			"retryBackoffMs":    config.SourceDefault,
			"renderThrottleMs":  config.SourceDefault,
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
		t.Error("expected shellTimeoutMs value in render output")
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

func TestSlashConfig_OpensOverlay(t *testing.T) {
	m := New(nil, "")
	m.SetSettings(config.Settings{
		ModelConfig: map[string]config.ModelConfig{
			"test": {Wire: config.WireAnthropic, ModelName: "m"},
		},
		DefaultModel: "test",
	})
	m.SetSlashCallbacks(SlashCallbacks{
		OpenConfig: func() (config.AnnotatedSettings, error) {
			return sampleAnnotated(), nil
		},
	})
	m.handleSlashCommand("/config")
	if m.activeModal == nil {
		t.Fatal("expected config overlay to be open after /config")
	}
}

func TestConfigEditMsg_Type(t *testing.T) {
	msg := ConfigEditMsg{Scope: "global", Err: nil}
	if msg.Scope != "global" {
		t.Errorf("got scope %q, want global", msg.Scope)
	}
}

func TestConfigOverlay_WizardAction(t *testing.T) {
	var editScope string
	co := NewConfigOverlay(sampleAnnotated(), func() {}, func(scope string) {
		editScope = scope
	})
	// Navigate to "Run setup wizard" (third item)
	co.HandleInput(tea.KeyMsg{Type: tea.KeyDown})
	co.HandleInput(tea.KeyMsg{Type: tea.KeyDown})
	co.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})
	if editScope != "wizard" {
		t.Errorf("expected wizard scope, got %q", editScope)
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

func renderedLines(co *ConfigOverlay) []string {
	return strings.Split(strings.Join(co.Render(80), "\n"), "\n")
}

func TestConfigOverlay_ClipsToMaxH(t *testing.T) {
	co := NewConfigOverlay(sampleAnnotated(), func() {}, nil)
	full := renderedLines(co)
	co.SetMaxH(8)
	got := renderedLines(co)
	if len(got) > 8 {
		t.Fatalf("got %d lines, want at most 8", len(got))
	}
	if len(full) <= 8 {
		t.Fatalf("sample config too short to test clipping: %d lines", len(full))
	}
	if !strings.Contains(got[0], "Configuration") {
		t.Errorf("expected view to start at the top, got %q", got[0])
	}
}

func TestConfigOverlay_NoClipWhenMaxHUnset(t *testing.T) {
	co := NewConfigOverlay(sampleAnnotated(), func() {}, nil)
	before := strings.Join(co.Render(80), "\n")
	co.SetMaxH(1000)
	after := strings.Join(co.Render(80), "\n")
	if before != after {
		t.Errorf("render changed when content fits:\n%s\n---\n%s", before, after)
	}
}

func TestConfigOverlay_PgDownPgUpScroll(t *testing.T) {
	co := NewConfigOverlay(sampleAnnotated(), func() {}, nil)
	co.SetMaxH(6)
	top := renderedLines(co)[0]
	co.HandleInput(tea.KeyMsg{Type: tea.KeyPgDown})
	scrolled := renderedLines(co)[0]
	if scrolled == top {
		t.Fatalf("expected PgDown to scroll, first line still %q", scrolled)
	}
	for range 10 {
		co.HandleInput(tea.KeyMsg{Type: tea.KeyPgUp})
	}
	if got := renderedLines(co)[0]; got != top {
		t.Errorf("after PgUp: got first line %q, want %q", got, top)
	}
}

func TestConfigOverlay_SelectionStaysVisible(t *testing.T) {
	co := NewConfigOverlay(sampleAnnotated(), func() {}, nil)
	co.SetMaxH(6)
	co.HandleInput(tea.KeyMsg{Type: tea.KeyDown})
	co.HandleInput(tea.KeyMsg{Type: tea.KeyDown})
	joined := strings.Join(renderedLines(co), "\n")
	if !strings.Contains(joined, "-> Run setup wizard") {
		t.Errorf("expected selected action visible after Down, got:\n%s", joined)
	}
	co.HandleInput(tea.KeyMsg{Type: tea.KeyUp})
	joined = strings.Join(renderedLines(co), "\n")
	if !strings.Contains(joined, "-> Edit project config") {
		t.Errorf("expected selected action visible after Up, got:\n%s", joined)
	}
}

func TestSlashConfig_SetsMaxHOnOpenAndResize(t *testing.T) {
	m := New(nil, "")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m.openConfigOverlayWith(sampleAnnotated())
	co, ok := m.activeModal.(*ConfigOverlay)
	if !ok {
		t.Fatal("expected config overlay to be open")
	}
	if co.maxH != m.approvalMaxHeight() || co.maxH <= 0 {
		t.Errorf("on open: maxH = %d, want %d", co.maxH, m.approvalMaxHeight())
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	if co.maxH != m.approvalMaxHeight() {
		t.Errorf("after resize: maxH = %d, want %d", co.maxH, m.approvalMaxHeight())
	}
}
