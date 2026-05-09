package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jhyoong/gohome/internal/config"
	"github.com/jhyoong/gohome/internal/llm"
)

func TestNonStreamingComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "hello back"}},
			},
		})
	}))
	defer srv.Close()

	client := llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"})
	resp, err := client.Complete(context.Background(), []llm.Message{{Role: "user", Content: "hello"}}, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "hello back" {
		t.Errorf("got %q", resp.Content)
	}
}

func TestStreamingTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\" there\"},\"finish_reason\":null}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"})
	var tokens []string
	var doneCalled bool
	err := client.Stream(context.Background(), []llm.Message{{Role: "user", Content: "hello"}}, nil,
		func(token string) { tokens = append(tokens, token) },
		func(_ []llm.ToolCall) {},
		func() { doneCalled = true },
		nil, // onUsage
	)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := ""
	for _, tk := range tokens {
		got += tk
	}
	if got != "hi there" {
		t.Errorf("got %q, want %q", got, "hi there")
	}
	if !doneCalled {
		t.Error("onDone not called")
	}
}

func TestStreamingUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify stream_options was sent
		var body struct {
			StreamOptions *struct {
				IncludeUsage bool `json:"include_usage"`
			} `json:"stream_options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body.StreamOptions == nil || !body.StreamOptions.IncludeUsage {
			t.Error("stream_options.include_usage not set in request")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		// Usage chunk: empty choices, usage field present
		w.Write([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := llm.NewClient(config.EndpointConfig{URL: srv.URL, Model: "test"})
	var gotPrompt, gotCompletion, gotTotal int
	err := client.Stream(context.Background(), []llm.Message{{Role: "user", Content: "hello"}}, nil,
		func(token string) {},
		func(_ []llm.ToolCall) {},
		func() {},
		func(prompt, completion, total int) {
			gotPrompt = prompt
			gotCompletion = completion
			gotTotal = total
		},
	)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if gotPrompt != 10 || gotCompletion != 5 || gotTotal != 15 {
		t.Errorf("usage: got (%d, %d, %d), want (10, 5, 15)", gotPrompt, gotCompletion, gotTotal)
	}
}
