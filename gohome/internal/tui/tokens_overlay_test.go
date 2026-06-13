package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

func TestTokensOverlay_Render(t *testing.T) {
	usage := common.Usage{
		InputTokens:      1234,
		OutputTokens:     567,
		CacheReadTokens:  89,
		CacheWriteTokens: 10,
	}
	sv := &SessionView{ID: "main", Usage: usage}
	overlay := NewTokensOverlay(sv, "claude-3-5-sonnet", 10000, func() {})
	lines := overlay.Render(80)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "1234") {
		t.Fatal("expected input tokens in render output")
	}
	if !strings.Contains(joined, "567") {
		t.Fatal("expected output tokens in render output")
	}
	if !strings.Contains(joined, "1801") {
		t.Fatal("expected total in render output")
	}
}

func TestTokensOverlay_EscCloses(t *testing.T) {
	closed := false
	sv := &SessionView{ID: "main"}
	overlay := NewTokensOverlay(sv, "test", 10000, func() { closed = true })
	overlay.HandleInput(tea.KeyMsg{Type: tea.KeyEsc})
	if !closed {
		t.Fatal("expected Esc to trigger close callback")
	}
}

func TestTokensOverlay_OtherKeysIgnored(t *testing.T) {
	closed := false
	sv := &SessionView{ID: "main"}
	overlay := NewTokensOverlay(sv, "test", 10000, func() { closed = true })
	overlay.HandleInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if closed {
		t.Fatal("expected non-Esc key to not trigger close")
	}
}
