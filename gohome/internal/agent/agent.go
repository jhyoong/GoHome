package agent

import (
	"sync/atomic"

	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// Agent drives a single agentic session: it owns the LLM client, tools,
// guardrail, frontend, and session writer, and orchestrates the turn loop.
type Agent struct {
	Client    common.Client
	Tools     *tools.Registry
	Guard     *guard.Guard
	Frontend  Frontend
	Writer    *session.Writer
	System    string
	MaxTokens int // if > 0, overrides the default 4096 per-turn token limit

	// Session is the session currently running inside Run. It is set at the
	// start of Run and used by Spawn to build the child session.
	Session *session.Session

	// Home is the gohome home directory used to compute subagent JSONL paths.
	Home string

	// subagentCounter is atomically incremented each time Spawn is called to
	// generate unique child IDs like "sub-1", "sub-2", ... per parent.
	subagentCounter atomic.Int32
}

// nextSubIndex atomically increments and returns the new subagent index.
func (a *Agent) nextSubIndex() int32 {
	return a.subagentCounter.Add(1)
}
