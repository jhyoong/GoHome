// Package llm provides a factory that builds the right wire-format adapter
// for a configured model config.
package llm

import (
	"fmt"

	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/llm/anthropic"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/llm/openai"
)

// New returns a common.Client for the model config's wire format.
// It returns an error for an unknown wire.
func New(e config.ModelConfig, apiKey string) (common.Client, error) {
	switch e.Wire {
	case config.WireAnthropic:
		return anthropic.New(e, apiKey), nil
	case config.WireOpenAI:
		return openai.New(e, apiKey), nil
	default:
		return nil, fmt.Errorf("unknown wire: %q", e.Wire)
	}
}
