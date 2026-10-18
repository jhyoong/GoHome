package common

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
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

// readAllPump reads the body to the end and emits one error event if the
// read fails, or one text event with the body otherwise.
func readAllPump(_ context.Context, body io.Reader) <-chan StreamEvent {
	ch := make(chan StreamEvent, 1)
	go func() {
		defer close(ch)
		b, err := io.ReadAll(body)
		if err != nil {
			ch <- StreamEvent{Kind: EventError, Err: err}
			return
		}
		ch <- StreamEvent{Kind: EventTextDelta, TextDelta: string(b)}
	}()
	return ch
}

func TestStreamRequest_IdleTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	ch, err := StreamRequest(context.Background(), StreamConfig{
		HTTPClient:  &http.Client{},
		Backoff:     []time.Duration{0},
		Prefix:      "test",
		IdleTimeout: 100 * time.Millisecond,
	}, func() (*http.Request, error) {
		return http.NewRequest(http.MethodPost, srv.URL, nil)
	}, readAllPump)
	if err != nil {
		t.Fatalf("StreamRequest error: %v", err)
	}

	timeout := time.After(5 * time.Second)
	var got []StreamEvent
	for done := false; !done; {
		select {
		case e, ok := <-ch:
			if !ok {
				done = true
				break
			}
			got = append(got, e)
		case <-timeout:
			t.Fatal("stream did not end after idle timeout")
		}
	}
	if len(got) != 1 || got[0].Kind != EventError {
		t.Fatalf("events: got %+v, want one error", got)
	}
	if !errors.Is(got[0].Err, ErrStreamIdle) {
		t.Errorf("error should wrap ErrStreamIdle: %v", got[0].Err)
	}
	if !strings.Contains(got[0].Err.Error(), "idle timeout") {
		t.Errorf("error should mention idle timeout: %v", got[0].Err)
	}
}

func TestStreamRequest_IdleTimerResetByData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		for i := 0; i < 5; i++ {
			_, _ = w.Write([]byte("x"))
			w.(http.Flusher).Flush()
			time.Sleep(40 * time.Millisecond)
		}
	}))
	defer srv.Close()

	ch, err := StreamRequest(context.Background(), StreamConfig{
		HTTPClient:  &http.Client{},
		Backoff:     []time.Duration{0},
		Prefix:      "test",
		IdleTimeout: 150 * time.Millisecond,
	}, func() (*http.Request, error) {
		return http.NewRequest(http.MethodPost, srv.URL, nil)
	}, readAllPump)
	if err != nil {
		t.Fatalf("StreamRequest error: %v", err)
	}
	var got []StreamEvent
	for e := range ch {
		got = append(got, e)
	}
	if len(got) != 1 || got[0].Kind != EventTextDelta || got[0].TextDelta != "xxxxx" {
		t.Errorf("events: got %+v, want text xxxxx", got)
	}
}

func TestStreamRequest_ErrorBodyCapped(t *testing.T) {
	big := strings.Repeat("y", 1<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, big, http.StatusBadRequest)
	}))
	defer srv.Close()

	_, err := StreamRequest(context.Background(), StreamConfig{
		HTTPClient: &http.Client{},
		Backoff:    []time.Duration{0},
		Prefix:     "test",
	}, func() (*http.Request, error) {
		return http.NewRequest(http.MethodPost, srv.URL, nil)
	}, readAllPump)
	if err == nil {
		t.Fatal("expected error")
	}
	if len(err.Error()) > MaxErrorBody+256 {
		t.Errorf("error message length %d exceeds cap %d", len(err.Error()), MaxErrorBody)
	}
}

func TestNewHTTPClient_Timeouts(t *testing.T) {
	hc := NewHTTPClient(DefaultResponseHeaderTimeout)
	if hc.Timeout != 0 {
		t.Errorf("Client.Timeout: got %v, want 0", hc.Timeout)
	}
	tr, ok := hc.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport: got %T, want *http.Transport", hc.Transport)
	}
	if tr.ResponseHeaderTimeout != DefaultResponseHeaderTimeout {
		t.Errorf("ResponseHeaderTimeout: got %v, want %v", tr.ResponseHeaderTimeout, DefaultResponseHeaderTimeout)
	}
	if DefaultResponseHeaderTimeout != 5*time.Minute || DefaultIdleTimeout != 5*time.Minute {
		t.Errorf("defaults: header %v idle %v, want 5m each", DefaultResponseHeaderTimeout, DefaultIdleTimeout)
	}
}

func TestNewHTTPClient_HeaderTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	// The context deadline stops the test from hanging if the header
	// timeout is missing; the assertion below then fails instead.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hc := NewHTTPClient(50 * time.Millisecond)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := hc.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected response header timeout error")
	}
	if !strings.Contains(err.Error(), "timeout awaiting response headers") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestStreamRequest_NoGoroutineLeak(t *testing.T) {
	before := runtime.NumGoroutine()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data"))
		w.(http.Flusher).Flush()
		if r.URL.Path == "/hang" {
			<-r.Context().Done()
		}
	}))
	hc := NewHTTPClient(time.Second)

	run := func(ctx context.Context, path string) <-chan StreamEvent {
		ch, err := StreamRequest(ctx, StreamConfig{
			HTTPClient:  hc,
			Backoff:     []time.Duration{0},
			Prefix:      "test",
			IdleTimeout: time.Minute,
		}, func() (*http.Request, error) {
			return http.NewRequest(http.MethodPost, srv.URL+path, nil)
		}, readAllPump)
		if err != nil {
			t.Fatalf("StreamRequest error: %v", err)
		}
		return ch
	}

	// Normal end.
	for range run(context.Background(), "/") {
	}
	// Caller cancels mid-stream.
	ctx, cancel := context.WithCancel(context.Background())
	ch := run(ctx, "/hang")
	cancel()
	for range ch {
	}

	hc.CloseIdleConnections()
	srv.Close()

	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before {
		buf := make([]byte, 1<<16)
		t.Errorf("goroutines: before %d, after %d\n%s", before, n, buf[:runtime.Stack(buf, true)])
	}
}

// TestStreamRequest_SlowConsumerNotIdle checks that time spent blocked on a
// slow consumer does not count as server silence.
func TestStreamRequest_SlowConsumerNotIdle(t *testing.T) {
	const n = 40
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", n)))
	}))
	defer srv.Close()

	// bytePump emits one event per byte, so the forwarder fills its output
	// buffer and blocks while the consumer sleeps.
	bytePump := func(_ context.Context, body io.Reader) <-chan StreamEvent {
		ch := make(chan StreamEvent)
		go func() {
			defer close(ch)
			buf := make([]byte, 1)
			for {
				k, err := body.Read(buf)
				if k > 0 {
					ch <- StreamEvent{Kind: EventTextDelta, TextDelta: string(buf[:k])}
				}
				if err != nil {
					if !errors.Is(err, io.EOF) {
						ch <- StreamEvent{Kind: EventError, Err: err}
					}
					return
				}
			}
		}()
		return ch
	}

	ch, err := StreamRequest(context.Background(), StreamConfig{
		HTTPClient:  &http.Client{},
		Backoff:     []time.Duration{0},
		Prefix:      "test",
		IdleTimeout: 100 * time.Millisecond,
	}, func() (*http.Request, error) {
		return http.NewRequest(http.MethodPost, srv.URL, nil)
	}, bytePump)
	if err != nil {
		t.Fatalf("StreamRequest error: %v", err)
	}

	time.Sleep(400 * time.Millisecond) // consumer is slower than the idle timeout
	var got []StreamEvent
	for e := range ch {
		got = append(got, e)
	}
	if len(got) != n {
		t.Fatalf("got %d events, want %d; last: %+v", len(got), n, got[len(got)-1])
	}
	for _, e := range got {
		if e.Kind != EventTextDelta {
			t.Fatalf("unexpected event: %+v", e)
		}
	}
}
