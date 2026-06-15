# Design: Deduplicate LLM Client HTTP/Stream Scaffold

## Problem

`gohome/internal/llm/anthropic/client.go` and `gohome/internal/llm/openai/client.go` are structurally identical: same `Client` struct, same `New()` constructor, same retry-wrapped HTTP call, same non-2xx error handling, same SSE pump goroutine. The only real differences are the URL path, auth headers, body builder, and error-message prefix. Both `retry.go` files are trivial aliases of `common.DefaultBackoff`.

A bug fix in the retry/pump logic must be applied twice. A third wire format would cost ~350 lines of copied scaffold.

## Approach

Extract a shared `StreamRequest` function into `common/stream.go`. Each adapter calls it with wire-specific callbacks. No new types, no inheritance, no embedded structs.

## Design

### New function: `common.StreamRequest`

```go
type StreamConfig struct {
    HTTPClient *http.Client
    Backoff    []time.Duration
    Prefix     string // "anthropic" or "openai"
}

func StreamRequest(
    ctx      context.Context,
    cfg      StreamConfig,
    buildReq func() (*http.Request, error),
    pump     func(ctx context.Context, body io.ReadCloser) <-chan StreamEvent,
) (<-chan StreamEvent, error)
```

**Behavior:**

1. Call `DoWithRetry(ctx, cfg.HTTPClient, cfg.Backoff, buildReq)` to get `resp`.
2. If `resp.StatusCode >= 400`: read body, return `fmt.Errorf("%s %d: %s", cfg.Prefix, statusCode, body)`.
3. Call `events := pump(ctx, resp.Body)`. The pump parses SSE and translates to `StreamEvent` values. It does not spawn a goroutine or close the body.
4. Spawn a forwarding goroutine that:
   - Defers `resp.Body.Close()`
   - Defers `close(out)`
   - Reads from `events`, writes to `out`, respects `ctx.Done()`
5. Return the `out` channel.

### Pump callback contract

Each adapter provides an unexported `pumpEvents` function:

```go
func pumpEvents(ctx context.Context, body io.ReadCloser) <-chan common.StreamEvent
```

This wires the adapter's `parseSSE(ctx, body)` into `translateEvents(ctx, frames)` and returns the translated channel. No goroutine needed -- `parseSSE` already spawns one internally, and `translateEvents` chains onto it.

### What each adapter keeps

- `request.go` -- wire-specific body building (unchanged)
- `sse.go` -- wire-specific SSE parsing (unchanged)
- `translate.go` -- wire-specific event translation (unchanged)
- `client.go` -- `Client` struct, `New()`, slimmed `Stream()` (~20 lines):
  1. Default the model
  2. Call `buildBody(req)`
  3. Call `common.StreamRequest(ctx, cfg, buildReq, pumpEvents)`

### What gets deleted

- `anthropic/retry.go` and `anthropic/retry_test.go`
- `openai/retry.go` and `openai/retry_test.go`

### What's new in `common/`

- `stream.go` -- `StreamConfig` struct + `StreamRequest` function (~40 lines)
- `stream_test.go` -- tests for happy path, 4xx error, context cancellation

## Testing strategy

- `common/stream_test.go` tests the shared scaffold with a mock HTTP server and trivial pump.
- Existing `anthropic/client_test.go` and `openai/client_test.go` test full end-to-end wiring (fixture -> Stream -> events). These catch any breakage from the refactor.
- All `sse_test.go`, `translate_test.go`, `request_test.go` files are unchanged.
- `common/retry_test.go` is unchanged.
- Verification: `go test ./gohome/...` and `go vet ./gohome/...`.

## Estimated impact

- ~+50 lines in `common/` (new file + tests)
- ~-70 lines per adapter (simplified `client.go`, deleted `retry.go`)
- Net reduction: ~90 lines
- Retry/error/pump logic exists in exactly one place
