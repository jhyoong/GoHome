package openai

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

// defaultRetryBackoff is the delay schedule used by Client.Stream between attempts.
var defaultRetryBackoff = []time.Duration{250 * time.Millisecond, time.Second, 2 * time.Second}

// Client implements common.Client for the OpenAI chat completions API.
type Client struct {
	base    string
	apiKey  string
	model   string
	headers map[string]string
	hc      *http.Client
	backoff []time.Duration
}

// New creates a Client from an Endpoint and resolved API key.
func New(e config.Endpoint, apiKey string) *Client {
	return &Client{
		base:    e.BaseURL,
		apiKey:  apiKey,
		model:   e.DefaultModel,
		headers: e.Headers,
		hc:      &http.Client{},
		backoff: defaultRetryBackoff,
	}
}

// Stream sends req to the OpenAI chat completions endpoint and returns a
// channel of StreamEvent values. On non-2xx responses it returns an error
// immediately. On success it spawns a goroutine that reads the SSE stream and
// forwards events. Transient 5xx and connection errors are retried according
// to the configured backoff schedule.
func (c *Client) Stream(ctx context.Context, req common.Request) (<-chan common.StreamEvent, error) {
	if req.Model == "" {
		req.Model = c.model
	}

	body, err := buildOpenAIBody(req)
	if err != nil {
		return nil, fmt.Errorf("openai: build body: %w", err)
	}

	resp, err := doWithRetry(ctx, c.hc, c.backoff, func() (*http.Request, error) {
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+c.apiKey)
		for k, v := range c.headers {
			r.Header.Set(k, v)
		}
		return r, nil
	})
	if err != nil {
		return nil, fmt.Errorf("openai: http: %w", err)
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai %d: %s", resp.StatusCode, errBody)
	}

	out := make(chan common.StreamEvent, 16)
	go func() {
		defer resp.Body.Close()
		defer close(out)

		frames := parseSSE(ctx, resp.Body)
		events := translateEvents(ctx, frames)

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
