package llm_test

import (
	"testing"

	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/llm"
	"github.com/jhyoong/GoHome/gohome/internal/llm/anthropic"
	"github.com/jhyoong/GoHome/gohome/internal/llm/openai"
)

func TestNew_Anthropic(t *testing.T) {
	c, err := llm.New(config.Endpoint{Wire: config.WireAnthropic, BaseURL: "http://x"}, "key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := c.(*anthropic.Client); !ok {
		t.Fatalf("want *anthropic.Client, got %T", c)
	}
}

func TestNew_OpenAI(t *testing.T) {
	c, err := llm.New(config.Endpoint{Wire: config.WireOpenAI, BaseURL: "http://x"}, "key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := c.(*openai.Client); !ok {
		t.Fatalf("want *openai.Client, got %T", c)
	}
}

func TestNew_UnknownWire(t *testing.T) {
	c, err := llm.New(config.Endpoint{Wire: "bogus"}, "key")
	if err == nil {
		t.Fatalf("want error for unknown wire, got client %v", c)
	}
	if c != nil {
		t.Fatalf("want nil client on error, got %T", c)
	}
}
