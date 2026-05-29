package anthropic

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// retryBackoff is the delay schedule between attempts (length == max retries - 1).
// It is a package-level variable so tests can override it to zero durations.
var retryBackoff = []time.Duration{250 * time.Millisecond, time.Second, 2 * time.Second}

// doWithRetry performs an HTTP request with retry on transient errors.
// It retries on connection errors and 5xx responses, but never on 4xx or context cancellation.
// The maximum number of attempts is len(retryBackoff)+1.
func doWithRetry(ctx context.Context, hc *http.Client, buildReq func() (*http.Request, error)) (*http.Response, error) {
	maxAttempts := len(retryBackoff) + 1
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Check context before each attempt.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Sleep before retry (not before the first attempt).
		if attempt > 0 {
			delay := retryBackoff[attempt-1]
			if delay > 0 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(delay):
				}
			}
		}

		req, err := buildReq()
		if err != nil {
			return nil, err
		}

		resp, err := hc.Do(req)
		if err != nil {
			// Connection-level error — retry unless context is cancelled.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = fmt.Errorf("attempt %d: %w", attempt+1, err)
			continue
		}

		// Never retry on 4xx.
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return resp, nil
		}

		// Retry on 5xx.
		if resp.StatusCode >= 500 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("anthropic %d: %s", resp.StatusCode, body)
			continue
		}

		// 2xx (or other non-error) — return as-is.
		return resp, nil
	}

	return nil, lastErr
}
