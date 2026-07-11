package config

import "time"

const (
	DefaultContextWindow    = 128_000
	DefaultMaxTokens        = 16_384
	DefaultThinkingBudget   = 10_240
	DefaultShellTimeoutMs    = 120_000
	DefaultMaxShellTimeoutMs = 600_000
	DefaultContextWarnPct   = 0.80
	DefaultContextCritPct   = 0.95
	DefaultRenderThrottleMs = 0
)

var DefaultRetryBackoff = []time.Duration{250 * time.Millisecond, time.Second, 2 * time.Second}
