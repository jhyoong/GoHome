package agent

import (
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// TestAgentStructFields is a compile-time + runtime sanity check that Agent
// has all the expected fields with the correct types.
func TestAgentStructFields(t *testing.T) {
	var c common.Client
	var reg *tools.Registry
	var g *guard.Guard
	var fe Frontend
	a := &Agent{
		Client:   c,
		Tools:    reg,
		Guard:    g,
		Frontend: fe,
		Writer:   nil,
		System:   "you are an assistant",
	}
	if a.System != "you are an assistant" {
		t.Errorf("System field: got %q", a.System)
	}
}
