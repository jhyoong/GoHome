package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/jhyoong/gohome/internal/config"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Response struct {
	Content   string
	ToolCalls []ToolCall
}

type Client struct {
	cfg  config.EndpointConfig
	http *http.Client
}

func NewClient(cfg config.EndpointConfig) *Client {
	return &Client{cfg: cfg, http: &http.Client{}}
}

func (c *Client) setAuth(req *http.Request) {
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
}

type reqBody struct {
	Model       string        `json:"model"`
	Messages    []Message     `json:"messages"`
	Tools       []interface{} `json:"tools,omitempty"`
	Stream      bool          `json:"stream"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

func (c *Client) Complete(ctx context.Context, messages []Message, tools []interface{}) (*Response, error) {
	body := reqBody{
		Model: c.cfg.Model, Messages: messages, Tools: tools,
		Stream: false, MaxTokens: c.cfg.MaxTokens, Temperature: c.cfg.Temperature,
	}
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.URL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("LLM request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("LLM returned %d: %s", resp.StatusCode, b)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content   string     `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}
	return &Response{
		Content:   result.Choices[0].Message.Content,
		ToolCalls: result.Choices[0].Message.ToolCalls,
	}, nil
}

func (c *Client) Stream(ctx context.Context, messages []Message, tools []interface{},
	onToken func(string), onToolCalls func([]ToolCall), onDone func()) error {

	body := reqBody{
		Model: c.cfg.Model, Messages: messages, Tools: tools,
		Stream: true, MaxTokens: c.cfg.MaxTokens, Temperature: c.cfg.Temperature,
	}
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.URL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("LLM stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("LLM returned %d: %s", resp.StatusCode, b)
	}

	toolBuf := make(map[int]*ToolCall)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]

		if choice.Delta.Content != "" {
			onToken(choice.Delta.Content)
		}

		for _, tc := range choice.Delta.ToolCalls {
			if _, ok := toolBuf[tc.Index]; !ok {
				toolBuf[tc.Index] = &ToolCall{ID: tc.ID, Type: tc.Type}
			}
			buf := toolBuf[tc.Index]
			buf.Function.Arguments += tc.Function.Arguments
			if tc.ID != "" {
				buf.ID = tc.ID
			}
			if tc.Function.Name != "" {
				buf.Function.Name = tc.Function.Name
			}
		}

		if choice.FinishReason != nil {
			switch *choice.FinishReason {
			case "tool_calls":
				idxs := make([]int, 0, len(toolBuf))
				for i := range toolBuf {
					idxs = append(idxs, i)
				}
				sort.Ints(idxs)
				calls := make([]ToolCall, len(idxs))
				for j, idx := range idxs {
					calls[j] = *toolBuf[idx]
				}
				onToolCalls(calls)
			case "stop":
				if onDone != nil {
					onDone()
				}
			}
		}
	}

	return scanner.Err()
}
