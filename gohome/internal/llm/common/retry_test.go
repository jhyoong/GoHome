package common

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDoWithRetry_503TwiceThen200(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	// zero backoff so the test is instant
	backoff := []time.Duration{0, 0, 0}
	resp, err := DoWithRetry(context.Background(), &http.Client{}, backoff, func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, srv.URL, nil)
	})
	if err != nil {
		t.Fatalf("DoWithRetry error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestDoWithRetry_NoRetryOn4xx(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	backoff := []time.Duration{0, 0, 0}
	resp, err := DoWithRetry(context.Background(), &http.Client{}, backoff, func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, srv.URL, nil)
	})
	if err != nil {
		t.Fatalf("DoWithRetry error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("expected 1 attempt (no retry on 4xx), got %d", atomic.LoadInt32(&attempts))
	}
}

func TestDoWithRetry_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	backoff := []time.Duration{0, 0, 0}
	_, err := DoWithRetry(ctx, &http.Client{}, backoff, func() (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// recordSleeps swaps the package sleep function for one that records each
// requested delay and returns at once. It restores the original on cleanup.
func recordSleeps(t *testing.T) *[]time.Duration {
	t.Helper()
	var got []time.Duration
	orig := sleepFn
	sleepFn = func(ctx context.Context, d time.Duration) error {
		got = append(got, d)
		return ctx.Err()
	}
	t.Cleanup(func() { sleepFn = orig })
	return &got
}

// noJitter makes jitter return the base delay unchanged.
func noJitter(t *testing.T) {
	t.Helper()
	orig := jitterFn
	jitterFn = func(d time.Duration) time.Duration { return d }
	t.Cleanup(func() { jitterFn = orig })
}

func failThenOK(status int, failures int32, header http.Header, attempts *int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(attempts, 1)
		if n <= failures {
			for k, v := range header {
				w.Header()[k] = v
			}
			http.Error(w, "fail", status)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

func TestDoWithRetry_RetryableStatuses(t *testing.T) {
	recordSleeps(t)
	for _, status := range []int{408, 409, 429, 500, 502, 503, 504, 529} {
		var attempts int32
		srv := httptest.NewServer(failThenOK(status, 1, nil, &attempts))
		resp, err := DoWithRetry(context.Background(), &http.Client{}, []time.Duration{0}, func() (*http.Request, error) {
			return http.NewRequest(http.MethodGet, srv.URL, nil)
		})
		if err != nil {
			t.Fatalf("status %d: DoWithRetry error: %v", status, err)
		}
		_ = resp.Body.Close()
		srv.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status %d: final status %d, want 200", status, resp.StatusCode)
		}
		if got := atomic.LoadInt32(&attempts); got != 2 {
			t.Errorf("status %d: attempts %d, want 2", status, got)
		}
	}
}

func TestDoWithRetry_NoRetryOnOther4xx(t *testing.T) {
	recordSleeps(t)
	for _, status := range []int{400, 401, 403, 404, 413, 422} {
		var attempts int32
		srv := httptest.NewServer(failThenOK(status, 1, nil, &attempts))
		resp, err := DoWithRetry(context.Background(), &http.Client{}, []time.Duration{0, 0}, func() (*http.Request, error) {
			return http.NewRequest(http.MethodGet, srv.URL, nil)
		})
		if err != nil {
			t.Fatalf("status %d: DoWithRetry error: %v", status, err)
		}
		_ = resp.Body.Close()
		srv.Close()
		if resp.StatusCode != status {
			t.Errorf("status %d: got %d", status, resp.StatusCode)
		}
		if got := atomic.LoadInt32(&attempts); got != 1 {
			t.Errorf("status %d: attempts %d, want 1", status, got)
		}
	}
}

func TestDoWithRetry_RetryAfterSeconds(t *testing.T) {
	sleeps := recordSleeps(t)
	noJitter(t)
	var attempts int32
	srv := httptest.NewServer(failThenOK(429, 1, http.Header{"Retry-After": {"7"}}, &attempts))
	defer srv.Close()

	resp, err := DoWithRetry(context.Background(), &http.Client{}, []time.Duration{250 * time.Millisecond}, func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, srv.URL, nil)
	})
	if err != nil {
		t.Fatalf("DoWithRetry error: %v", err)
	}
	_ = resp.Body.Close()
	if len(*sleeps) != 1 || (*sleeps)[0] != 7*time.Second {
		t.Errorf("sleeps: got %v, want [7s]", *sleeps)
	}
}

func TestDoWithRetry_RetryAfterHTTPDate(t *testing.T) {
	sleeps := recordSleeps(t)
	noJitter(t)
	date := time.Now().Add(20 * time.Second).UTC().Format(http.TimeFormat)
	var attempts int32
	srv := httptest.NewServer(failThenOK(503, 1, http.Header{"Retry-After": {date}}, &attempts))
	defer srv.Close()

	resp, err := DoWithRetry(context.Background(), &http.Client{}, []time.Duration{250 * time.Millisecond}, func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, srv.URL, nil)
	})
	if err != nil {
		t.Fatalf("DoWithRetry error: %v", err)
	}
	_ = resp.Body.Close()
	// HTTP dates have one-second resolution, so allow a small window.
	if len(*sleeps) != 1 || (*sleeps)[0] < 17*time.Second || (*sleeps)[0] > 20*time.Second {
		t.Errorf("sleeps: got %v, want about 20s", *sleeps)
	}
}

func TestDoWithRetry_RetryAfterCapped(t *testing.T) {
	sleeps := recordSleeps(t)
	noJitter(t)
	var attempts int32
	srv := httptest.NewServer(failThenOK(429, 1, http.Header{"Retry-After": {"3600"}}, &attempts))
	defer srv.Close()

	resp, err := DoWithRetry(context.Background(), &http.Client{}, []time.Duration{0}, func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, srv.URL, nil)
	})
	if err != nil {
		t.Fatalf("DoWithRetry error: %v", err)
	}
	_ = resp.Body.Close()
	if len(*sleeps) != 1 || (*sleeps)[0] != MaxRetryAfter {
		t.Errorf("sleeps: got %v, want [%v]", *sleeps, MaxRetryAfter)
	}
}

func TestDoWithRetry_JitterAppliedToBackoff(t *testing.T) {
	sleeps := recordSleeps(t)
	orig := jitterFn
	jitterFn = func(d time.Duration) time.Duration { return d + time.Millisecond }
	t.Cleanup(func() { jitterFn = orig })

	var attempts int32
	srv := httptest.NewServer(failThenOK(503, 2, nil, &attempts))
	defer srv.Close()

	resp, err := DoWithRetry(context.Background(), &http.Client{}, []time.Duration{100 * time.Millisecond, 200 * time.Millisecond}, func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, srv.URL, nil)
	})
	if err != nil {
		t.Fatalf("DoWithRetry error: %v", err)
	}
	_ = resp.Body.Close()
	want := []time.Duration{101 * time.Millisecond, 201 * time.Millisecond}
	if len(*sleeps) != 2 || (*sleeps)[0] != want[0] || (*sleeps)[1] != want[1] {
		t.Errorf("sleeps: got %v, want %v", *sleeps, want)
	}
}

func TestDefaultJitter_Bounded(t *testing.T) {
	d := time.Second
	for i := 0; i < 1000; i++ {
		got := defaultJitter(d)
		if got < d || got >= d+d/2 {
			t.Fatalf("defaultJitter(%v) = %v, want in [%v, %v)", d, got, d, d+d/2)
		}
	}
	if got := defaultJitter(0); got != 0 {
		t.Errorf("defaultJitter(0) = %v, want 0", got)
	}
}

func TestDoWithRetry_ContextCancelDuringSleep(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "busy", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	start := time.Now()
	_, err := DoWithRetry(ctx, &http.Client{}, []time.Duration{time.Hour}, func() (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	})
	if err == nil {
		t.Fatal("expected error after cancel")
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("DoWithRetry did not stop sleeping on cancel")
	}
}

func TestDoWithRetry_ErrorBodyCapped(t *testing.T) {
	recordSleeps(t)
	big := strings.Repeat("x", 1<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, big, http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := DoWithRetry(context.Background(), &http.Client{}, []time.Duration{0}, func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, srv.URL, nil)
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if len(err.Error()) > MaxErrorBody+256 {
		t.Errorf("error message length %d exceeds cap %d", len(err.Error()), MaxErrorBody)
	}
}
