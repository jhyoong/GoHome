package config

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

func TestModelConfig_ParseThinking(t *testing.T) {
	var mc ModelConfig
	if err := json.Unmarshal([]byte(`{"thinking":"budget","thinkingBudget":2048}`), &mc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if mc.Thinking != "budget" || mc.ThinkingBudget != 2048 {
		t.Errorf("got thinking=%q budget=%d, want budget/2048", mc.Thinking, mc.ThinkingBudget)
	}
}

func TestResolveThinking(t *testing.T) {
	tests := []struct {
		name       string
		cfg        ModelConfig
		wantMode   common.ThinkingMode
		wantBudget int
	}{
		{"unset no budget is adaptive", ModelConfig{}, common.ThinkingModeAdaptive, 0},
		{"unset with budget is budget", ModelConfig{ThinkingBudget: 4096}, common.ThinkingModeBudget, 4096},
		{"off", ModelConfig{Thinking: "off", ThinkingBudget: 4096}, common.ThinkingModeOff, 0},
		{"adaptive ignores budget", ModelConfig{Thinking: "adaptive", ThinkingBudget: 4096}, common.ThinkingModeAdaptive, 0},
		{"budget default", ModelConfig{Thinking: "budget"}, common.ThinkingModeBudget, DefaultThinkingBudget},
		{"budget explicit", ModelConfig{Thinking: "budget", ThinkingBudget: 1024, MaxTokens: 2048}, common.ThinkingModeBudget, 1024},
		{"openai wire skips budget checks", ModelConfig{Wire: WireOpenAI, ThinkingBudget: 20000}, common.ThinkingModeBudget, 20000},
		{"adaptive skips budget checks", ModelConfig{Thinking: "adaptive", ThinkingBudget: 10, MaxTokens: 5}, common.ThinkingModeAdaptive, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cfg.Wire == "" {
				tt.cfg.Wire = WireAnthropic
			}
			mode, budget, err := ResolveThinking("m1", tt.cfg)
			if err != nil {
				t.Fatalf("ResolveThinking: %v", err)
			}
			if mode != tt.wantMode || budget != tt.wantBudget {
				t.Errorf("got %q/%d, want %q/%d", mode, budget, tt.wantMode, tt.wantBudget)
			}
		})
	}
}

func TestResolveThinking_Errors(t *testing.T) {
	tests := []struct {
		name     string
		cfg      ModelConfig
		wantSubs []string
	}{
		{"unknown mode", ModelConfig{Thinking: "enabled"}, []string{`"m1"`, `"enabled"`, "off", "adaptive", "budget"}},
		{"budget too small", ModelConfig{Thinking: "budget", ThinkingBudget: 512}, []string{`"m1"`, "512", "1024"}},
		{"unset budget too small", ModelConfig{ThinkingBudget: 100}, []string{`"m1"`, "100", "1024"}},
		{"budget equals maxTokens", ModelConfig{Thinking: "budget", ThinkingBudget: 4096, MaxTokens: 4096}, []string{`"m1"`, "4096", "maxTokens"}},
		{"default budget over default maxTokens", ModelConfig{Thinking: "budget", MaxTokens: 8000}, []string{`"m1"`, "10240", "8000"}},
		{"budget over default maxTokens", ModelConfig{ThinkingBudget: 20000}, []string{`"m1"`, "20000", "16384"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.Wire = WireAnthropic
			_, _, err := ResolveThinking("m1", tt.cfg)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			for _, sub := range tt.wantSubs {
				if !strings.Contains(err.Error(), sub) {
					t.Errorf("error %q does not contain %q", err.Error(), sub)
				}
			}
		})
	}
}
