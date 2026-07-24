package headless_test

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/headless"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// fakeClient is a minimal common.Client that returns a canned response.
type fakeClient struct {
	response string
}

func (c *fakeClient) Stream(_ context.Context, _ common.Request) (<-chan common.StreamEvent, error) {
	ch := make(chan common.StreamEvent, 3)
	ch <- common.StreamEvent{Kind: common.EventTextDelta, TextDelta: c.response}
	ch <- common.StreamEvent{Kind: common.EventTurnDone, StopReason: "end_turn", Usage: &common.Usage{InputTokens: 10, OutputTokens: 5}}
	close(ch)
	return ch, nil
}

func TestHeadless_EndToEnd(t *testing.T) {
	var buf bytes.Buffer
	fe := headless.NewFrontend("say hello", false, &buf)

	client := &fakeClient{response: "Hello!"}
	dir := t.TempDir()
	sess := session.NewSession("test-id", dir, "fake-model", "fake")
	writerPath := filepath.Join(dir, "test.jsonl")
	writer, err := session.OpenWriter(writerPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Close() }()

	wl, _ := guard.LoadWhitelist("", "")
	g := guard.NewGuard(wl, fe, nil)
	g.SetYolo(true)

	registry := tools.NewRegistry()
	state := agent.NewSessionState(sess, writer, client)

	a := &agent.Agent{
		Tools:    registry,
		Guard:    g,
		Frontend: fe,
		State:    state,
		System:   "You are a test agent.",
	}

	sess.History = append(sess.History, common.Message{
		Role:    common.RoleUser,
		Content: []common.Block{{Kind: common.BlockText, Text: "say hello"}},
	})

	err = a.Run(context.Background(), sess)
	if err != nil {
		t.Fatalf("agent.Run failed: %v", err)
	}

	got := fe.FinalText()
	if got != "Hello!" {
		t.Errorf("FinalText() = %q, want %q", got, "Hello!")
	}
}

func TestHeadless_Interactive_MultiTurn(t *testing.T) {
	input := strings.NewReader(
		"{\"type\":\"user_message\",\"content\":\"say hello\"}\n" +
			"{\"type\":\"user_message\",\"content\":\"say goodbye\"}\n" +
			"{\"type\":\"exit\"}\n",
	)
	var buf bytes.Buffer
	fe := headless.NewInteractiveFrontend(input, true, &buf)

	client := &fakeClient{response: "Hello!"}
	dir := t.TempDir()
	sess := session.NewSession("test-interactive", dir, "fake-model", "fake")
	writerPath := filepath.Join(dir, "test.jsonl")
	writer, err := session.OpenWriter(writerPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Close() }()

	wl, _ := guard.LoadWhitelist("", "")
	g := guard.NewGuard(wl, fe, nil)
	g.SetYolo(true)

	registry := tools.NewRegistry()
	state := agent.NewSessionState(sess, writer, client)

	a := &agent.Agent{
		Tools:    registry,
		Guard:    g,
		Frontend: fe,
		State:    state,
		System:   "You are a test agent.",
	}

	ctx := context.Background()
	turnCount := 0
	for {
		text, inputErr := fe.AwaitUserInput(ctx)
		if inputErr != nil {
			break
		}
		sess.History = append(sess.History, common.Message{
			Role:    common.RoleUser,
			Content: []common.Block{{Kind: common.BlockText, Text: text}},
		})
		writer.Emit(session.UserMessage{
			Content: []common.Block{{Kind: common.BlockText, Text: text}},
		})
		runErr := a.Run(ctx, sess)
		writer.Emit(session.TurnDone{SessionID: sess.ID})
		if runErr != nil {
			t.Fatalf("agent.Run failed on turn %d: %v", turnCount+1, runErr)
		}
		turnCount++
	}

	if turnCount != 2 {
		t.Errorf("expected 2 turns, got %d", turnCount)
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		t.Errorf("expected output for 2 turns, got %d lines", len(lines))
	}
}
