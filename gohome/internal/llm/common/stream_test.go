package common

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStreamRequest_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	pump := func(_ context.Context, _ io.Reader) <-chan StreamEvent {
		ch := make(chan StreamEvent, 1)
		ch <- StreamEvent{Kind: EventTextDelta, TextDelta: "hello"}
		close(ch)
		return ch
	}

	ch, err := StreamRequest(context.Background(), StreamConfig{
		HTTPClient: &http.Client{},
		Backoff:    []time.Duration{0},
		Prefix:     "test",
	}, func() (*http.Request, error) {
		return http.NewRequest(http.MethodPost, srv.URL, nil)
	}, pump)
	if err != nil {
		t.Fatalf("StreamRequest error: %v", err)
	}

	var got []StreamEvent
	for e := range ch {
		got = append(got, e)
	}
	if len(got) != 1 || got[0].Kind != EventTextDelta || got[0].TextDelta != "hello" {
		t.Errorf("unexpected events: %+v", got)
	}
}

func TestStreamRequest_4xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	pump := func(_ context.Context, _ io.Reader) <-chan StreamEvent {
		t.Fatal("pump should not be called on 4xx")
		return nil
	}

	_, err := StreamRequest(context.Background(), StreamConfig{
		HTTPClient: &http.Client{},
		Backoff:    []time.Duration{0},
		Prefix:     "test",
	}, func() (*http.Request, error) {
		return http.NewRequest(http.MethodPost, srv.URL, nil)
	}, pump)
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !strings.Contains(err.Error(), "test 401") {
		t.Errorf("error should contain prefix and status code: %v", err)
	}
}

func TestStreamRequest_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())

	pump := func(ctx context.Context, _ io.Reader) <-chan StreamEvent {
		ch := make(chan StreamEvent)
		go func() {
			<-ctx.Done()
			close(ch)
		}()
		return ch
	}

	ch, err := StreamRequest(ctx, StreamConfig{
		HTTPClient: &http.Client{},
		Backoff:    []time.Duration{0},
		Prefix:     "test",
	}, func() (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, nil)
	}, pump)
	if err != nil {
		t.Fatalf("StreamRequest error: %v", err)
	}

	cancel()

	for range ch {
	}
}
