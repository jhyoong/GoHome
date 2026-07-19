package headless

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
)

var _ agent.Frontend = (*Frontend)(nil)

func TestNewFrontend(t *testing.T) {
	var buf bytes.Buffer
	fe := NewFrontend("hello", false, &buf)
	if fe == nil {
		t.Fatal("NewFrontend returned nil")
	}
}

func TestAwaitUserInput_FirstCall(t *testing.T) {
	var buf bytes.Buffer
	fe := NewFrontend("do something", false, &buf)

	ctx := context.Background()
	text, err := fe.AwaitUserInput(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "do something" {
		t.Errorf("got %q, want %q", text, "do something")
	}
}

func TestAwaitUserInput_SecondCall_Blocks(t *testing.T) {
	var buf bytes.Buffer
	fe := NewFrontend("hello", false, &buf)

	ctx := context.Background()
	_, _ = fe.AwaitUserInput(ctx) // consume first call

	ctx2, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := fe.AwaitUserInput(ctx2)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestEmit_PlainText_AccumulatesTokenDeltas(t *testing.T) {
	var buf bytes.Buffer
	fe := NewFrontend("x", false, &buf)

	fe.Emit("s1", agent.Event{Kind: agent.EventTokenDelta, TextDelta: "Hello"})
	fe.Emit("s1", agent.Event{Kind: agent.EventTokenDelta, TextDelta: " world"})
	fe.Emit("s1", agent.Event{Kind: agent.EventSending})
	fe.Emit("s1", agent.Event{Kind: agent.EventTurnDone})

	got := fe.FinalText()
	if got != "Hello world" {
		t.Errorf("FinalText() = %q, want %q", got, "Hello world")
	}

	if buf.Len() != 0 {
		t.Errorf("expected no output to writer in plain mode, got %q", buf.String())
	}
}

func TestEmit_Verbose_WritesJSONLines(t *testing.T) {
	var buf bytes.Buffer
	fe := NewFrontend("x", true, &buf)

	fe.Emit("s1", agent.Event{Kind: agent.EventTokenDelta, TextDelta: "hi"})
	fe.Emit("s1", agent.Event{Kind: agent.EventTurnDone, StopReason: "end_turn"})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}

	var ev1 map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &ev1); err != nil {
		t.Fatalf("line 1 not valid JSON: %v", err)
	}
	if ev1["kind"] != "token_delta" {
		t.Errorf("line 1 kind = %v, want token_delta", ev1["kind"])
	}
}

func TestRequestApproval_ReturnsAllowOnce(t *testing.T) {
	var buf bytes.Buffer
	fe := NewFrontend("x", false, &buf)

	dec, err := fe.RequestApproval(context.Background(), guard.ApprovalRequest{
		Tool:    "shell",
		Summary: "rm -rf /",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Outcome != guard.AllowOnce {
		t.Errorf("outcome = %v, want AllowOnce", dec.Outcome)
	}
}
