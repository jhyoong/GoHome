package agent

import (
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// Agent drives a single agentic session: it owns the LLM client, tools,
// guardrail, frontend, and session writer, and orchestrates the turn loop.
type Agent struct {
	Client   common.Client
	Tools    *tools.Registry
	Guard    *guard.Guard
	Frontend Frontend
	Writer   *session.Writer
	System   string
}
