package common

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultBackoff is the base delay schedule used between retry attempts.
// Jitter is added to each delay unless the server sends Retry-After.
var DefaultBackoff = []time.Duration{250 * time.Millisecond, time.Second, 2 * time.Second}

// MaxRetryAfter caps the delay taken from a Retry-After header.
const MaxRetryAfter = 60 * time.Second

// MaxErrorBody caps how many bytes of an error response body are read.
const MaxErrorBody = 64 << 10

// jitterFn and sleepFn are variables so tests can make retries deterministic
// and instant.
var (
	jitterFn = defaultJitter
	sleepFn  = sleepCtx
)

// defaultJitter returns a random delay in [d, 1.5d).
func defaultJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	return d + rand.N(d/2+1)
}

// sleepCtx waits for d or until ctx is done, whichever comes first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// retryableStatus reports whether a response status is worth retrying:
// 408, 409, 429 and every 5xx (including 529 overloaded).
func retryableStatus(code int) bool {
	switch code {
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooManyRequests:
		return true
	}
	return code >= 500
}

// parseRetryAfter reads a Retry-After value given as integer seconds or an
// HTTP date. It returns false when the header is absent or invalid. The
// result is capped at MaxRetryAfter and never negative.
func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	var d time.Duration
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		if secs < 0 {
			return 0, false
		}
		if secs > int64(MaxRetryAfter/time.Second) {
			return MaxRetryAfter, true
		}
		d = time.Duration(secs) * time.Second
	} else if t, err := http.ParseTime(v); err == nil {
		d = t.Sub(now)
	} else {
		return 0, false
	}
	return min(max(d, 0), MaxRetryAfter), true
}

// DoWithRetry performs an HTTP request with retry on transient errors.
// It retries connection errors and 408, 409, 429 and 5xx responses, but not
// other 4xx statuses or context cancellation. The maximum number of attempts
// is len(backoff)+1. Before each retry it waits for the Retry-After delay if
// the server sent one, otherwise for backoff[i] plus jitter.
// Retry applies only to getting a response; a stream that fails after the
// body is handed back is not retried.
func DoWithRetry(ctx context.Context, hc *http.Client, backoff []time.Duration, buildReq func() (*http.Request, error)) (*http.Response, error) {
	maxAttempts := len(backoff) + 1
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		req, err := buildReq()
		if err != nil {
			return nil, err
		}

		resp, err := hc.Do(req)
		var delay time.Duration
		switch {
		case err != nil:
			// Connection-level error: retry unless the context is done.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = fmt.Errorf("attempt %d: %w", attempt+1, err)
			if attempt < len(backoff) {
				delay = jitterFn(backoff[attempt])
			}
		case retryableStatus(resp.StatusCode):
			// Drain a capped amount of the body and close it before retrying.
			body, _ := io.ReadAll(io.LimitReader(resp.Body, MaxErrorBody))
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("http %d: %s", resp.StatusCode, body)
			if ra, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); ok {
				delay = ra
			} else if attempt < len(backoff) {
				delay = jitterFn(backoff[attempt])
			}
		default:
			// 2xx or a non-retryable 4xx: the caller handles it.
			return resp, nil
		}

		if attempt == maxAttempts-1 {
			break
		}
		if err := sleepFn(ctx, delay); err != nil {
			return nil, err
		}
	}

	return nil, lastErr
}
