package anthropic

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

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
}

// New creates a Client from an Endpoint and resolved API key.
func New(e config.Endpoint, apiKey string) *Client {
	return &Client{
		base:    e.BaseURL,
		apiKey:  apiKey,
		model:   e.DefaultModel,
		headers: e.Headers,
		hc:      &http.Client{},
	}
}

// Stream sends req to Anthropic and returns a channel of StreamEvent values.
// On non-2xx responses it returns an error immediately.
// On success it spawns a goroutine that reads the SSE stream and forwards events.
func (c *Client) Stream(ctx context.Context, req common.Request) (<-chan common.StreamEvent, error) {
	if req.Model == "" {
		req.Model = c.model
	}

	body, err := buildAnthropicBody(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic: build body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anthropic: create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Anthropic-Version", anthropicVersion)
	httpReq.Header.Set("X-API-Key", c.apiKey)
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.hc.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: http: %w", err)
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic %d: %s", resp.StatusCode, errBody)
	}

	out := make(chan common.StreamEvent, 16)
	go func() {
		defer resp.Body.Close()
		defer close(out)

		frames := parseSSE(resp.Body)
		events := translateEvents(frames)

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
