package anthropic

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

const anthropicVersion = "2023-06-01"

// Client implements common.Client for the Anthropic Messages API.
type Client struct {
	base    string
	apiKey  string
	model   string
	headers map[string]string
	hc      *http.Client
	backoff []time.Duration
}

// New creates a Client from a ModelConfig and resolved API key.
func New(e config.ModelConfig, apiKey string) *Client {
	return &Client{
		base:    e.BaseURL,
		apiKey:  apiKey,
		model:   e.ModelName,
		headers: e.Headers,
		hc:      &http.Client{},
		backoff: common.DefaultBackoff,
	}
}

// Stream sends req to Anthropic and returns a channel of StreamEvent values.
// It delegates to common.StreamRequest for retry, error handling, and goroutine
// management.
func (c *Client) Stream(ctx context.Context, req common.Request) (<-chan common.StreamEvent, error) {
	if req.Model == "" {
		req.Model = c.model
	}

	body, err := buildAnthropicBody(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic: build body: %w", err)
	}

	return common.StreamRequest(ctx, common.StreamConfig{
		HTTPClient: c.hc,
		Backoff:    c.backoff,
		Prefix:     "anthropic",
	}, func() (*http.Request, error) {
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/messages", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Anthropic-Version", anthropicVersion)
		r.Header.Set("X-API-Key", c.apiKey)
		for k, v := range c.headers {
			r.Header.Set(k, v)
		}
		return r, nil
	}, pumpEvents)
}

// pumpEvents wires parseSSE -> translateEvents for the Anthropic SSE stream.
func pumpEvents(ctx context.Context, body io.Reader) <-chan common.StreamEvent {
	frames := parseSSE(ctx, body)
	return translateEvents(ctx, frames)
}
