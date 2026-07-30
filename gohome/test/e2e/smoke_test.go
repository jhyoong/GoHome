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
	"sync"
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

// e2eConfig holds the environment variables needed for E2E tests.
type e2eConfig struct {
	baseURL string
	wire    config.Wire
	model   string
	apiKey  string
}

// loadE2EConfig reads environment variables and skips if not set.
func loadE2EConfig(t *testing.T) e2eConfig {
	t.Helper()
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
	return e2eConfig{baseURL: baseURL, wire: wire, model: model, apiKey: apiKey}
}

// recordingFrontend implements agent.Frontend and records events.
type recordingFrontend struct {
	mu     sync.Mutex
	events []agent.Event
}

func (r *recordingFrontend) Emit(_ string, ev agent.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *recordingFrontend) RequestApproval(_ context.Context, _ guard.ApprovalRequest) (guard.ApprovalDecision, error) {
	return guard.ApprovalDecision{Outcome: guard.AllowOnce}, nil
}

func (r *recordingFrontend) AwaitUserInput(_ context.Context) (string, error) {
	return "", errors.New("no interactive input in e2e tests")
}

// newE2EAgent creates an Agent wired up for E2E testing.
func newE2EAgent(t *testing.T, cfg e2eConfig, fe agent.Frontend, history []common.Message) (*agent.Agent, *session.Session) {
	t.Helper()
	ep := config.ModelConfig{
		Wire:      cfg.wire,
		BaseURL:   cfg.baseURL,
		ModelName: cfg.model,
	}
	client, err := llm.New(ep, cfg.apiKey)
	if err != nil {
		t.Fatalf("llm.New: %v", err)
	}

	reg := tools.NewRegistry()

	wl, err := guard.Compile(guard.WhitelistFile{}, guard.WhitelistFile{}, "")
	if err != nil {
		t.Fatalf("guard.Compile: %v", err)
	}
	g := guard.NewGuard(wl, fe, nil)
	g.SetYolo(true)

	tmpDir := t.TempDir()
	writerPath := filepath.Join(tmpDir, "e2e.jsonl")
	w, err := session.OpenWriter(writerPath)
	if err != nil {
		t.Fatalf("session.OpenWriter: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	sess := session.NewSession("e2e-test", tmpDir, cfg.model, string(cfg.wire))
	sess.History = history

	state := agent.NewSessionState(sess, w, client)
	a := &agent.Agent{
		Tools:     reg,
		Guard:     g,
		Frontend:  fe,
		State:     state,
		System:    "You are a helpful assistant.",
		MaxTokens: 256,
	}

	return a, sess
}

func TestE2ESmokeRoundtrip(t *testing.T) {
	cfg := loadE2EConfig(t)
	fe := &recordingFrontend{}
	history := []common.Message{
		{
			Role: common.RoleUser,
			Content: []common.Block{
				{Kind: common.BlockText, Text: "Reply with the single word: pong."},
			},
		},
	}

	a, sess := newE2EAgent(t, cfg, fe, history)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := a.Run(ctx, sess); err != nil {
		t.Fatalf("agent.Run: %v", err)
	}

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
		t.Error("last assistant message text is empty")
	}
	t.Logf("assistant replied: %q", lastAssistantText)
}
