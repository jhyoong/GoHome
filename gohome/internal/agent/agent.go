package agent

import (
	"sync"
	"sync/atomic"

	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// Agent drives a single agentic session: it owns the tools, guardrail,
// frontend, and session state, and orchestrates the turn loop.
type Agent struct {
	Tools          *tools.Registry
	Guard          *guard.Guard
	State          *SessionState
	System         string
	MaxTokens      int // if > 0, overrides the default 4096 per-turn token limit
	ThinkingBudget int // if > 0, enable extended thinking with this token budget

	// Home is the gohome home directory used to compute subagent JSONL paths.
	Home string

	mu       sync.Mutex
	frontend Frontend

	// subagentCounter is atomically incremented each time Spawn is called to
	// generate unique child IDs like "sub-1", "sub-2", ... per parent.
	subagentCounter atomic.Int32
}

// SetFrontend sets the agent's frontend under a mutex for concurrent safety.
func (a *Agent) SetFrontend(fe Frontend) {
	a.mu.Lock()
	a.frontend = fe
	a.mu.Unlock()
}

// Frontend returns the agent's current frontend under a mutex for concurrent safety.
func (a *Agent) Frontend() Frontend {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.frontend
}

// nextSubIndex atomically increments and returns the new subagent index.
func (a *Agent) nextSubIndex() int32 {
	return a.subagentCounter.Add(1)
}
