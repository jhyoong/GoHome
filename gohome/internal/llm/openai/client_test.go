package openai

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
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		// Verify Bearer auth header.
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
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
		DefaultModel: "gpt-4o",
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

	// The fixture has 3 non-empty text deltas: "Hello", ", world", "!"
	// (the first chunk has content="" and should be skipped)
	expected := []string{"Hello", ", world", "!"}
	if len(textDeltas) != len(expected) {
		t.Errorf("expected %d text deltas, got %d: %v", len(expected), len(textDeltas), textDeltas)
	}
	for i, want := range expected {
		if i < len(textDeltas) && textDeltas[i] != want {
			t.Errorf("delta %d: got %q, want %q", i, textDeltas[i], want)
		}
	}

	if turnDone == nil {
		t.Fatal("no EventTurnDone received")
	}
	if turnDone.StopReason != "stop" {
		t.Errorf("StopReason: got %q, want stop", turnDone.StopReason)
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
		DefaultModel: "gpt-4o",
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

func TestClientStream_CustomHeaders(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Custom-Header")
		// Return minimal valid SSE so the stream can finish.
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	ep := config.Endpoint{
		BaseURL:      srv.URL,
		DefaultModel: "gpt-4o",
		Headers:      map[string]string{"X-Custom-Header": "myvalue"},
	}
	client := New(ep, "test-key")

	req := common.Request{
		Messages:  []common.Message{{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}}},
		MaxTokens: 100,
	}

	ch, err := client.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream error: %v", err)
	}
	for range ch {
	}

	if gotHeader != "myvalue" {
		t.Errorf("X-Custom-Header: got %q, want myvalue", gotHeader)
	}
}
