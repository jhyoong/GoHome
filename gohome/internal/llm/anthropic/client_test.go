package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

func serveFixture(t *testing.T, fixture string) *httptest.Server {
	t.Helper()
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}))
}

func TestClientStream_TextFixture(t *testing.T) {
	srv := serveFixture(t, "testdata/simple_text.sse")
	defer srv.Close()

	ep := config.Endpoint{
		BaseURL:      srv.URL,
		DefaultModel: "claude-3-5-haiku-20241022",
	}
	client := New(ep, "test-key")

	req := common.Request{
		System:    "You are helpful.",
		Messages:  []common.Message{{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}}},
		MaxTokens: 100,
	}

	ch, err := client.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream error: %v", err)
	}

	var textDeltas []string
	var turnDone *common.StreamEvent
	for e := range ch {
		if e.Kind == common.EventError {
			t.Fatalf("stream error event: %v", e.Err)
		}
		if e.Kind == common.EventTextDelta {
			textDeltas = append(textDeltas, e.TextDelta)
		}
		if e.Kind == common.EventTurnDone {
			cp := e
			turnDone = &cp
		}
	}

	if len(textDeltas) != 3 {
		t.Errorf("expected 3 text deltas, got %d: %v", len(textDeltas), textDeltas)
	}
	expected := []string{"Hello", ", world", "!"}
	for i, d := range expected {
		if i < len(textDeltas) && textDeltas[i] != d {
			t.Errorf("delta %d: got %q, want %q", i, textDeltas[i], d)
		}
	}

	if turnDone == nil {
		t.Fatal("no EventTurnDone received")
	}
	if turnDone.StopReason != "end_turn" {
		t.Errorf("StopReason: got %q, want end_turn", turnDone.StopReason)
	}
	if turnDone.Usage == nil {
		t.Fatal("Usage is nil on TurnDone")
	}
	if turnDone.Usage.InputTokens != 10 {
		t.Errorf("InputTokens: got %d, want 10", turnDone.Usage.InputTokens)
	}
	if turnDone.Usage.OutputTokens != 15 {
		t.Errorf("OutputTokens: got %d, want 15", turnDone.Usage.OutputTokens)
	}
}

func TestClientStream_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	ep := config.Endpoint{
		BaseURL:      srv.URL,
		DefaultModel: "claude-3-5-haiku-20241022",
	}
	client := New(ep, "bad-key")

	req := common.Request{
		Messages:  []common.Message{{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}}},
		MaxTokens: 100,
	}

	_, err := client.Stream(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}
