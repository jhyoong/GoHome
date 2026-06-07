package config

import "time"

const (
	DefaultContextWindow    = 128_000
	DefaultMaxTokens        = 16_384
	DefaultThinkingBudget   = 10_240
	DefaultBashTimeoutMs    = 120_000
	DefaultMaxBashTimeoutMs = 600_000
	DefaultContextWarnPct   = 0.80
	DefaultContextCritPct   = 0.95
)

var DefaultRetryBackoff = []time.Duration{250 * time.Millisecond, time.Second, 2 * time.Second}
