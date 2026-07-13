package config

import "time"

const (
	DefaultContextWindow     = 128_000
	DefaultMaxTokens         = 16_384
	DefaultThinkingBudget    = 10_240
	DefaultShellTimeoutMs    = 120_000
	DefaultMaxShellTimeoutMs = 600_000
	DefaultContextWarnPct    = 0.80
	DefaultContextCritPct    = 0.95
	DefaultRenderThrottleMs  = 0

	DefaultAutoCompactPct       = 0.80
	DefaultAutoCompactTargetPct = 0.50
	DefaultAutoCompactLeftover  = 32_000
)

var DefaultRetryBackoff = []time.Duration{250 * time.Millisecond, time.Second, 2 * time.Second}

var DefaultAutoCompactPrompt = `You are summarizing a coding assistant conversation for context compaction.
Produce a concise summary that preserves:
- The user's current goal and any sub-tasks
- Key decisions made and their reasoning
- File paths and code changes discussed or made
- Any pending work or unresolved issues
- Tool results that are still relevant

Be factual and specific. Do not add commentary or analysis.
Write the summary as a narrative, not a bulleted list.`
