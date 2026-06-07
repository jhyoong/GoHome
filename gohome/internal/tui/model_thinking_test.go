package tui

import (
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
)

func TestHandleAgentEvent_ThinkingDoneCollapsesEntry(t *testing.T) {
	m := New(nil, "sess-1")
	m.winW = 80
	m.winH = 40

	// Simulate a thinking delta arriving (creates an expanded thinking entry).
	m.handleAgentEvent(AgentEventMsg{
		SessionID: "sess-1",
		Ev: agent.Event{
			Kind:          agent.EventThinkingDelta,
			ThinkingDelta: "line one\nline two\nline three",
		},
	})

	sv := m.sessions["sess-1"]
	if len(sv.Timeline) != 1 {
		t.Fatalf("timeline len = %d, want 1", len(sv.Timeline))
	}
	if sv.Timeline[0].Kind != KindThinking {
		t.Fatalf("entry kind = %q, want %q", sv.Timeline[0].Kind, KindThinking)
	}
	if !sv.Timeline[0].Expanded {
		t.Fatal("thinking entry should be expanded during streaming")
	}

	// Simulate thinking done.
	m.handleAgentEvent(AgentEventMsg{
		SessionID: "sess-1",
		Ev:        agent.Event{Kind: agent.EventThinkingDone},
	})

	if sv.Timeline[0].Expanded {
		t.Error("thinking entry should be collapsed after EventThinkingDone")
	}
}
