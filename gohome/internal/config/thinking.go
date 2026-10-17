package config

import (
	"fmt"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// ResolveThinking returns the thinking mode and budget for model config cfg
// (named name), and checks them.
//
// An unset thinking value means budget mode when thinkingBudget > 0, and
// adaptive mode otherwise. In budget mode an unset budget gets
// DefaultThinkingBudget, and the budget must be at least MinThinkingBudget and
// below maxTokens (DefaultMaxTokens when unset). These checks apply only to the
// anthropic wire, the only one that sends the budget. The budget is 0 in other
// modes.
func ResolveThinking(name string, cfg ModelConfig) (common.ThinkingMode, int, error) {
	mode := common.ThinkingMode(cfg.Thinking)
	switch mode {
	case "":
		if cfg.ThinkingBudget > 0 {
			mode = common.ThinkingModeBudget
		} else {
			mode = common.ThinkingModeAdaptive
		}
	case common.ThinkingModeOff, common.ThinkingModeAdaptive, common.ThinkingModeBudget:
	default:
		return "", 0, fmt.Errorf("model config %q: unknown thinking value %q (use \"off\", \"adaptive\" or \"budget\")", name, cfg.Thinking)
	}
	// Only the Anthropic wire sends the budget, so other wires skip the checks.
	if cfg.Wire != WireAnthropic {
		if mode == common.ThinkingModeBudget {
			return mode, cfg.ThinkingBudget, nil
		}
		return mode, 0, nil
	}
	if mode != common.ThinkingModeBudget {
		return mode, 0, nil
	}

	budget := cfg.ThinkingBudget
	if budget <= 0 {
		budget = DefaultThinkingBudget
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	if budget < MinThinkingBudget {
		return "", 0, fmt.Errorf("model config %q: thinkingBudget %d is below the minimum of %d", name, budget, MinThinkingBudget)
	}
	if budget >= maxTokens {
		return "", 0, fmt.Errorf("model config %q: thinkingBudget %d must be below maxTokens %d", name, budget, maxTokens)
	}
	return mode, budget, nil
}
