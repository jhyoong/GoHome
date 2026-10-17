package llm_test

import (
	"testing"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/llm"
	"github.com/jhyoong/GoHome/gohome/internal/llm/anthropic"
	"github.com/jhyoong/GoHome/gohome/internal/llm/openai"
)

func TestNew_Anthropic(t *testing.T) {
	c, err := llm.New(config.ModelConfig{Wire: config.WireAnthropic, BaseURL: "http://x"}, "key", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := c.(*anthropic.Client); !ok {
		t.Fatalf("want *anthropic.Client, got %T", c)
	}
}

func TestNew_OpenAI(t *testing.T) {
	c, err := llm.New(config.ModelConfig{Wire: config.WireOpenAI, BaseURL: "http://x"}, "key", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := c.(*openai.Client); !ok {
		t.Fatalf("want *openai.Client, got %T", c)
	}
}

func TestNew_UnknownWire(t *testing.T) {
	c, err := llm.New(config.ModelConfig{Wire: "bogus"}, "key", nil)
	if err == nil {
		t.Fatalf("want error for unknown wire, got client %v", c)
	}
	if c != nil {
		t.Fatalf("want nil client on error, got %T", c)
	}
}

func TestNew_BackoffFromSettings(t *testing.T) {
	got := llm.Backoff([]int{10, 20})
	if len(got) != 2 || got[0] != 10*time.Millisecond || got[1] != 20*time.Millisecond {
		t.Errorf("got %v", got)
	}
	for _, in := range [][]int{nil, {}, {-1}} {
		def := llm.Backoff(in)
		if len(def) != len(config.DefaultRetryBackoff) || def[0] != config.DefaultRetryBackoff[0] {
			t.Errorf("Backoff(%v) = %v, want default", in, def)
		}
	}
}
