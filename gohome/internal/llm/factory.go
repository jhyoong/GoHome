// Package llm provides a factory that builds the right wire-format adapter
// for a configured model config.
package llm

import (
	"fmt"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/llm/anthropic"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/llm/openai"
)

// Backoff converts the retryBackoffMs setting to a retry schedule. It returns
// config.DefaultRetryBackoff when the setting is empty or has a negative value.
func Backoff(ms []int) []time.Duration {
	if len(ms) == 0 {
		return config.DefaultRetryBackoff
	}
	out := make([]time.Duration, len(ms))
	for i, v := range ms {
		if v < 0 {
			return config.DefaultRetryBackoff
		}
		out[i] = time.Duration(v) * time.Millisecond
	}
	return out
}

// New returns a common.Client for the model config's wire format.
// retryBackoffMs is the retry schedule in milliseconds; empty means the default.
// It returns an error for an unknown wire.
func New(e config.ModelConfig, apiKey string, retryBackoffMs []int) (common.Client, error) {
	backoff := Backoff(retryBackoffMs)
	switch e.Wire {
	case config.WireAnthropic:
		return anthropic.New(e, apiKey, backoff), nil
	case config.WireOpenAI:
		return openai.New(e, apiKey, backoff), nil
	default:
		return nil, fmt.Errorf("unknown wire: %q", e.Wire)
	}
}
