package agent

import (
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// Agent drives a single agentic session: it owns the tools, guardrail,
// frontend, and session state, and orchestrates the turn loop.
type Agent struct {
	Tools         *tools.Registry
	Guard         *guard.Guard
	Frontend      Frontend
	State         *SessionState
	System        string
	CompactCfg    CompactConfig
	CompactPrompt string

	// compactWait holds back compaction after a rejected summary.
	compactWait compactWait

	// Home is the gohome home directory used to compute subagent JSONL paths.
	Home string
}
