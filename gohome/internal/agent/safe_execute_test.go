package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// TestRun_PanicTool verifies that a panicking tool does NOT crash Run: it
// produces an IsError tool result containing "tool panicked", and the loop
// continues to the next turn (which ends normally).
func TestRun_PanicTool(t *testing.T) {
	turn1 := []common.StreamEvent{
		{Kind: common.EventToolCallDone, ToolCallID: "tc-panic", ToolName: "panicktool", InputJSON: `{}`},
		{Kind: common.EventTurnDone, StopReason: "tool_use"},
	}
	turn2 := []common.StreamEvent{
		{Kind: common.EventTurnDone, StopReason: "end_turn"},
	}
	client := &fakeClient{sequences: [][]common.StreamEvent{turn1, turn2}}

	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "panicktool", panics: true})

	fe := &fakeRecorder{}
	g := compileYoloGuard(t)
	a, sess := newTestAgentWithGuard(t, client, fe, g, reg)

	// Run must not panic and must return nil.
	if err := a.Run(context.Background(), sess); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Find the RoleTool message and check IsError + content.
	var foundPanicResult bool
	for _, msg := range sess.History {
		if msg.Role == common.RoleTool {
			for _, b := range msg.Content {
				if b.IsError && strings.Contains(b.ResultText, "tool panicked") {
					foundPanicResult = true
				}
			}
		}
	}
	if !foundPanicResult {
		t.Errorf("expected an IsError block containing 'tool panicked' in history")
	}

	// Frontend should also have received the EventToolResult with IsError.
	var sawErrResult bool
	for _, ev := range fe.events {
		if ev.Kind == EventToolResult && ev.Result != nil && ev.Result.IsError &&
			strings.Contains(ev.Result.Content, "tool panicked") {
			sawErrResult = true
		}
	}
	if !sawErrResult {
		t.Errorf("frontend did not receive IsError EventToolResult for panicking tool")
	}
}

// TestSafeExecute_Panic verifies the safeExecute helper directly.
func TestSafeExecute_Panic(t *testing.T) {
	tool := &fakeTool{name: "panicky", panics: true}
	res, err := safeExecute(context.Background(), tool, nil, tools.NullSink{})
	if err != nil {
		t.Errorf("safeExecute: unexpected err %v", err)
	}
	if !res.IsError {
		t.Errorf("expected IsError=true")
	}
	if !strings.Contains(res.Content, "tool panicked") {
		t.Errorf("expected 'tool panicked' in content, got %q", res.Content)
	}
}
