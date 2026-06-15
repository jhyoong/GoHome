package common

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// StreamConfig holds the shared configuration for streaming HTTP requests.
type StreamConfig struct {
	HTTPClient *http.Client
	Backoff    []time.Duration
	Prefix     string
}

// StreamRequest performs an HTTP request with retry, checks for error status
// codes, then hands the response body to the provided pump function which
// parses the SSE stream into StreamEvents. The returned channel is closed
// when the pump finishes or the context is cancelled.
func StreamRequest(
	ctx context.Context,
	cfg StreamConfig,
	buildReq func() (*http.Request, error),
	pump func(ctx context.Context, body io.Reader) <-chan StreamEvent,
) (<-chan StreamEvent, error) {
	resp, err := DoWithRetry(ctx, cfg.HTTPClient, cfg.Backoff, buildReq)
	if err != nil {
		return nil, fmt.Errorf("%s: http: %w", cfg.Prefix, err)
	}

	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s %d: %s", cfg.Prefix, resp.StatusCode, errBody)
	}

	events := pump(ctx, resp.Body)

	out := make(chan StreamEvent, 16)
	go func() {
		defer func() { _ = resp.Body.Close() }()
		defer close(out)

		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-events:
				if !ok {
					return
				}
				select {
				case out <- e:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return out, nil
}
