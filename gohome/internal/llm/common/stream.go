package common

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ErrStreamIdle is wrapped by the read error returned when a streamed
// response body sends no bytes for the idle timeout.
var ErrStreamIdle = errors.New("stream idle timeout")

// ErrStreamEndedEarly is returned when a stream closes before the response is
// reported as complete.
var ErrStreamEndedEarly = errors.New("stream ended early")

// StreamConfig holds the shared configuration for streaming HTTP requests.
type StreamConfig struct {
	HTTPClient *http.Client
	Backoff    []time.Duration
	Prefix     string
	// IdleTimeout cancels the request when the response body sends no bytes
	// for this long. Zero means DefaultIdleTimeout.
	IdleTimeout time.Duration
}

// idleReader resets an idle timer on every read that returns data. When the
// timer fires, the request context is cancelled with an ErrStreamIdle cause,
// and the reader reports that cause instead of a bare "context canceled".
type idleReader struct {
	r       io.Reader
	ctx     context.Context
	timer   *time.Timer
	timeout time.Duration
}

func (ir *idleReader) Read(p []byte) (int, error) {
	n, err := ir.r.Read(p)
	if n > 0 {
		ir.timer.Reset(ir.timeout)
	}
	if err != nil {
		if cause := context.Cause(ir.ctx); errors.Is(cause, ErrStreamIdle) {
			err = cause
		}
	}
	return n, err
}

// StreamRequest performs an HTTP request with retry, checks for error status
// codes, then hands the response body to the provided pump function which
// parses the SSE stream into StreamEvents. The returned channel is closed
// when the pump finishes or the context is cancelled. If the body sends no
// bytes for cfg.IdleTimeout, the request is cancelled and the pump sees a
// read error wrapping ErrStreamIdle.
func StreamRequest(
	ctx context.Context,
	cfg StreamConfig,
	buildReq func() (*http.Request, error),
	pump func(ctx context.Context, body io.Reader) <-chan StreamEvent,
) (<-chan StreamEvent, error) {
	idle := cfg.IdleTimeout
	if idle <= 0 {
		idle = DefaultIdleTimeout
	}

	// reqCtx lets the idle watchdog cancel the request without cancelling
	// the caller's context.
	reqCtx, cancel := context.WithCancelCause(ctx)
	resp, err := DoWithRetry(ctx, cfg.HTTPClient, cfg.Backoff, func() (*http.Request, error) {
		r, err := buildReq()
		if err != nil {
			return nil, err
		}
		return r.WithContext(reqCtx), nil
	})
	if err != nil {
		cancel(nil)
		return nil, fmt.Errorf("%s: http: %w", cfg.Prefix, err)
	}

	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, MaxErrorBody))
		_ = resp.Body.Close()
		cancel(nil)
		return nil, fmt.Errorf("%s %d: %s", cfg.Prefix, resp.StatusCode, errBody)
	}

	idleErr := fmt.Errorf("%s: %w: no data received for %s", cfg.Prefix, ErrStreamIdle, idle)
	timer := time.AfterFunc(idle, func() { cancel(idleErr) })

	events := pump(ctx, &idleReader{r: resp.Body, ctx: reqCtx, timer: timer, timeout: idle})

	out := make(chan StreamEvent, 16)
	go func() {
		// Stop the watchdog and release reqCtx however the stream ends.
		defer cancel(nil)
		defer timer.Stop()
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
