package agent

import "github.com/jhyoong/GoHome/gohome/internal/tools"

// Compile-time assertion: *Agent satisfies tools.SubagentSpawner.
var _ tools.SubagentSpawner = (*Agent)(nil)

// RegisterSubagentTool registers the "subagent" tool on a.Tools using a itself
// as the spawner. Call this after constructing an Agent that should support
// spawning isolated child agents.
func (a *Agent) RegisterSubagentTool() {
	a.Tools.Register(tools.NewSubagentTool(a))
}
