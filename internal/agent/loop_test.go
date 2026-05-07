package agent_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JiaHui/gohome/internal/agent"
	"github.com/JiaHui/gohome/internal/approval"
	"github.com/JiaHui/gohome/internal/config"
	"github.com/JiaHui/gohome/internal/llm"
	"github.com/JiaHui/gohome/internal/session"
	"github.com/JiaHui/gohome/internal/tools"
)

func TestSimpleMessageRoundtrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"world\"},\"finish_reason\":null}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	store, _ := session.Open(t.TempDir() + "/test.db")
	defer store.Close()
	ctx := context.Background()
	sess, _ := store.CreateSession(ctx)

	llmClient := llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"})
	reg := tools.NewRegistry()
	broker := approval.NewBroker(config.ApprovalConfig{}, nil)
	loop := agent.NewLoop(llmClient, reg, store, "")

	var tokens []string
	err := loop.Run(ctx, sess.ID, "tab-1", "hello", broker,
		func(tok string) { tokens = append(tokens, tok) },
		func(msg string) {},
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	result := ""
	for _, tk := range tokens {
		result += tk
	}
	if result != "world" {
		t.Errorf("got %q, want %q", result, "world")
	}

	msgs, _ := store.GetMessages(ctx, sess.ID)
	if len(msgs) < 1 || msgs[0].Content != "hello" {
		t.Errorf("user message not persisted: %+v", msgs)
	}
}
