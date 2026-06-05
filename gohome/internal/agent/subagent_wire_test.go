package agent

import (
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// TestRegisterSubagentTool verifies that after calling RegisterSubagentTool:
//   - a.Tools.Get("subagent") returns the tool (ok == true).
//   - a.Tools.Without("subagent") does NOT contain the subagent tool.
func TestRegisterSubagentTool(t *testing.T) {
	g := compileYoloGuard(t)
	fe := &fakeRecorder{}

	a := &Agent{
		Client:   nil, // not needed for this test
		Tools:    tools.NewRegistry(),
		Guard:    g,
		Frontend: fe,
		System:   "sys",
	}

	// Before registration, "subagent" must not exist.
	if _, ok := a.Tools.Get("subagent"); ok {
		t.Fatal("expected 'subagent' to be absent before RegisterSubagentTool")
	}

	a.RegisterSubagentTool()

	// After registration, "subagent" must exist.
	tool, ok := a.Tools.Get("subagent")
	if !ok {
		t.Fatal("expected 'subagent' to be present after RegisterSubagentTool")
	}
	if tool.Name() != "subagent" {
		t.Errorf("tool.Name(): got %q, want %q", tool.Name(), "subagent")
	}

	// Without("subagent") must NOT include the subagent tool.
	child := a.Tools.Without("subagent")
	if _, ok := child.Get("subagent"); ok {
		t.Errorf("Without('subagent') still contains 'subagent'")
	}

	// Original registry must still have it (Without does not mutate).
	if _, ok := a.Tools.Get("subagent"); !ok {
		t.Errorf("original registry lost 'subagent' after Without call")
	}
}

// TestRegisterSubagentTool_SatisfiesSpawnerInterface is a compile-time check
// that *Agent satisfies tools.SubagentSpawner. If this file compiles, the
// check passes; the runtime assertion is redundant but makes intent explicit.
func TestRegisterSubagentTool_SatisfiesSpawnerInterface(t *testing.T) {
	var _ tools.SubagentSpawner = (*Agent)(nil)
}
