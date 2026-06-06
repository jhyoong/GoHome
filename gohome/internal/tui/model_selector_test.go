package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/config"
)

func sampleEndpoints() map[string]config.Endpoint {
	return map[string]config.Endpoint{
		"anthropic": {
			Wire:         config.WireAnthropic,
			DefaultModel: "claude-sonnet-4-20250514",
		},
		"openai": {
			Wire:         config.WireOpenAI,
			DefaultModel: "gpt-4o",
		},
	}
}

func TestModelSelectorRender(t *testing.T) {
	ms := NewModelSelector(sampleEndpoints(), "anthropic")
	lines := ms.Render(80)
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines (search + 2 items), got %d", len(lines))
	}
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "anthropic") {
		t.Error("should show 'anthropic' endpoint")
	}
}

func TestModelSelectorCurrentFirst(t *testing.T) {
	ms := NewModelSelector(sampleEndpoints(), "openai")
	lines := ms.Render(80)
	firstItem := StripAnsi(lines[1])
	if !strings.Contains(firstItem, "openai") {
		t.Errorf("current endpoint should be listed first: %q", firstItem)
	}
}

func TestModelSelectorCurrentMarked(t *testing.T) {
	ms := NewModelSelector(sampleEndpoints(), "anthropic")
	lines := ms.Render(80)
	joined := StripAnsi(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "(current)") {
		t.Error("current endpoint should be marked with (current)")
	}
}

func TestModelSelectorSelectReturnsModelName(t *testing.T) {
	var gotEndpoint, gotModel string
	ms := NewModelSelector(sampleEndpoints(), "anthropic")
	ms.SetOnSelect(func(endpoint, model string) {
		gotEndpoint = endpoint
		gotModel = model
	})
	ms.list.HandleInput(tea.KeyMsg{Type: tea.KeyDown})
	ms.list.HandleInput(tea.KeyMsg{Type: tea.KeyEnter})
	if gotEndpoint == "" || gotModel == "" {
		t.Errorf("expected endpoint and model, got endpoint=%q model=%q", gotEndpoint, gotModel)
	}
}

func TestModelSelectorCancel(t *testing.T) {
	cancelled := false
	ms := NewModelSelector(sampleEndpoints(), "anthropic")
	ms.SetOnCancel(func() { cancelled = true })
	ms.list.HandleInput(tea.KeyMsg{Type: tea.KeyEsc})
	if !cancelled {
		t.Error("Escape should call onCancel")
	}
}
