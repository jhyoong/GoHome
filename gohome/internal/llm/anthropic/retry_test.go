package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

func TestClientStream_RetryOn5xx(t *testing.T) {
	fixtureData, err := os.ReadFile("testdata/simple_text.sse")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			// first two attempts: 503
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		// third attempt: success
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixtureData)
	}))
	defer srv.Close()

	ep := config.Endpoint{
		BaseURL:      srv.URL,
		DefaultModel: "claude-3-5-haiku-20241022",
	}
	client := New(ep, "test-key")
	client.backoff = []time.Duration{0, 0, 0}

	req := common.Request{
		Messages:  []common.Message{{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}}},
		MaxTokens: 100,
	}

	ch, err := client.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream error after retries: %v", err)
	}

	// drain the channel
	for e := range ch {
		if e.Kind == common.EventError {
			t.Fatalf("stream error event: %v", e.Err)
		}
	}

	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestClientStream_NoRetryOn4xx(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	ep := config.Endpoint{
		BaseURL:      srv.URL,
		DefaultModel: "claude-3-5-haiku-20241022",
	}
	client := New(ep, "test-key")
	client.backoff = []time.Duration{0, 0, 0}

	req := common.Request{
		Messages:  []common.Message{{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}}},
		MaxTokens: 100,
	}

	_, err := client.Stream(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}

	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("expected 1 attempt (no retry on 4xx), got %d", atomic.LoadInt32(&attempts))
	}
}

func TestClientStream_NoRetryOnContextCancel(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ep := config.Endpoint{
		BaseURL:      srv.URL,
		DefaultModel: "claude-3-5-haiku-20241022",
	}
	client := New(ep, "test-key")
	client.backoff = []time.Duration{0, 0, 0}

	req := common.Request{
		Messages:  []common.Message{{Role: common.RoleUser, Content: []common.Block{{Kind: common.BlockText, Text: "hi"}}}},
		MaxTokens: 100,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := client.Stream(ctx, req)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}

	// should not have retried after context cancel
	a := atomic.LoadInt32(&attempts)
	if a > 1 {
		t.Errorf("expected at most 1 attempt with cancelled context, got %d", a)
	}
}
