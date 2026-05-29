//go:build e2e

// Package e2e contains opt-in end-to-end tests that require a live LLM
// endpoint. These tests are never run in CI; pass -tags e2e to enable them.
//
// Required environment variables:
//
//	GOHOME_E2E_ENDPOINT  base URL of the LLM endpoint (e.g. https://api.anthropic.com)
//	GOHOME_E2E_WIRE      wire format: "anthropic" or "openai"
//	GOHOME_E2E_MODEL     model name (e.g. claude-opus-4-7)
//	GOHOME_E2E_API_KEY   API key for the endpoint
//
// If GOHOME_E2E_ENDPOINT is unset, the test skips.
package e2e

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/llm"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// noopFrontend is a minimal agent.Frontend for headless e2e runs.
// It discards all events, approves every tool call with AllowOnce,
// and returns an error on AwaitUserInput (so the agent never blocks
// waiting for interactive input).
type noopFrontend struct{}

func (noopFrontend) Emit(_ string, _ agent.Event) {}

func (noopFrontend) RequestApproval(_ context.Context, _ guard.ApprovalRequest) (guard.ApprovalDecision, error) {
	return guard.ApprovalDecision{Outcome: guard.AllowOnce}, nil
}

func (noopFrontend) AwaitUserInput(_ context.Context, _ string) (string, error) {
	return "", errors.New("no interactive input in e2e tests")
}

func TestE2ESmokeRoundtrip(t *testing.T) {
	baseURL := os.Getenv("GOHOME_E2E_ENDPOINT")
	if baseURL == "" {
		t.Skip("GOHOME_E2E_ENDPOINT not set; skipping e2e test")
	}

	wire := config.Wire(os.Getenv("GOHOME_E2E_WIRE"))
	if wire == "" {
		wire = config.WireAnthropic
	}
	model := os.Getenv("GOHOME_E2E_MODEL")
	if model == "" {
		t.Fatal("GOHOME_E2E_MODEL must be set")
	}
	apiKey := os.Getenv("GOHOME_E2E_API_KEY")
	if apiKey == "" {
		t.Fatal("GOHOME_E2E_API_KEY must be set")
	}

	ep := config.Endpoint{
		Wire:         wire,
		BaseURL:      baseURL,
		DefaultModel: model,
	}

	client, err := llm.New(ep, apiKey)
	if err != nil {
		t.Fatalf("llm.New: %v", err)
	}

	reg := tools.NewRegistry()

	wl, err := guard.Compile(guard.WhitelistFile{}, guard.WhitelistFile{}, "")
	if err != nil {
		t.Fatalf("guard.Compile: %v", err)
	}
	fe := noopFrontend{}
	g := guard.NewGuard(wl, fe)
	g.SetYolo(true)

	tmpDir := t.TempDir()
	writerPath := filepath.Join(tmpDir, "e2e.jsonl")
	w, err := session.OpenWriter(writerPath)
	if err != nil {
		t.Fatalf("session.OpenWriter: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	sess := session.NewSession("e2e-smoke", tmpDir, model, string(wire))
	// Seed with a deterministic user message.
	sess.History = []common.Message{
		{
			Role: common.RoleUser,
			Content: []common.Block{
				{Kind: common.BlockText, Text: "Reply with the single word: pong."},
			},
		},
	}

	a := &agent.Agent{
		Client:    client,
		Tools:     reg,
		Guard:     g,
		Frontend:  fe,
		Writer:    w,
		System:    "You are a helpful assistant.",
		MaxTokens: 64,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := a.Run(ctx, sess); err != nil {
		t.Fatalf("agent.Run: %v", err)
	}

	// Find the last assistant message and check it has non-empty text.
	var lastAssistantText string
	for _, msg := range sess.History {
		if msg.Role == common.RoleAssistant {
			for _, b := range msg.Content {
				if b.Kind == common.BlockText {
					lastAssistantText = b.Text
				}
			}
		}
	}
	if lastAssistantText == "" {
		t.Error("last assistant message text is empty; expected a non-empty reply")
	}
	t.Logf("assistant replied: %q", lastAssistantText)
}
