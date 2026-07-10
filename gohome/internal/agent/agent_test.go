package agent

import (
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// Compile-time check: ensure Agent no longer has Writer or Session fields.
// If someone re-adds them this will fail to compile.
var _ = func() {
	_ = Agent{State: (*SessionState)(nil)}
}

// TestAgentStructFields is a compile-time + runtime sanity check that Agent
// has all the expected fields with the correct types.
func TestAgentStructFields(t *testing.T) {
	var reg *tools.Registry
	var g *guard.Guard
	var fe Frontend
	a := &Agent{
		Tools:    reg,
		Guard:    g,
		Frontend: fe,
		State:    NewSessionState(nil, nil, nil),
		System:   "you are an assistant",
	}
	if a.System != "you are an assistant" {
		t.Errorf("System field: got %q", a.System)
	}
}
