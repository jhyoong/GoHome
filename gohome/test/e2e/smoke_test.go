//go:build e2e

// Package e2e contains opt-in end-to-end tests that require a live LLM
// endpoint. These tests are never run in CI; pass -tags e2e to enable them.
//
// Configuration is loaded from e2e.config.json (copy e2e.config.example.json
// to get started). Environment variables override the config file:
//
//	GOHOME_E2E_ENDPOINT  base URL of the LLM endpoint
//	GOHOME_E2E_WIRE      wire format: "anthropic" or "openai"
//	GOHOME_E2E_MODEL     model name
//	GOHOME_E2E_API_KEY   API key for the endpoint
//
// If no endpoint is configured from either source, the tests skip.
package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/headless"
	"github.com/jhyoong/GoHome/gohome/internal/llm"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// e2eConfig holds the configuration needed for E2E tests.
type e2eConfig struct {
	baseURL string
	wire    config.Wire
	model   string
	apiKey  string
}

type e2eConfigFile struct {
	Endpoint string `json:"endpoint"`
	Wire     string `json:"wire"`
	Model    string `json:"model"`
	APIKey   string `json:"api_key"`
}

// loadE2EConfig loads config from e2e.config.json (next to this test file),
// then lets environment variables override any field. Skips if no endpoint
// is configured from either source.
func loadE2EConfig(t *testing.T) e2eConfig {
	t.Helper()

	var file e2eConfigFile
	data, err := os.ReadFile(filepath.Join(".", "e2e.config.json"))
	if err == nil {
		if jerr := json.Unmarshal(data, &file); jerr != nil {
			t.Fatalf("e2e.config.json: %v", jerr)
		}
	}

	baseURL := envOr("GOHOME_E2E_ENDPOINT", file.Endpoint)
	if baseURL == "" {
		t.Skip("no e2e endpoint configured (set GOHOME_E2E_ENDPOINT or create e2e.config.json)")
	}
	wire := config.Wire(envOr("GOHOME_E2E_WIRE", file.Wire))
	if wire == "" {
		wire = config.WireAnthropic
	}
	model := envOr("GOHOME_E2E_MODEL", file.Model)
	if model == "" {
		t.Fatal("GOHOME_E2E_MODEL must be set (env or e2e.config.json)")
	}
	apiKey := envOr("GOHOME_E2E_API_KEY", file.APIKey)
	if apiKey == "" {
		t.Fatal("GOHOME_E2E_API_KEY must be set (env or e2e.config.json)")
	}
	return e2eConfig{baseURL: baseURL, wire: wire, model: model, apiKey: apiKey}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
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
func newE2EAgent(t *testing.T, cfg e2eConfig, fe agent.Frontend, history []common.Message, extraTools ...tools.Tool) (*agent.Agent, *session.Session) {
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
	reg.Register(tools.ReadTool{})
	for _, tool := range extraTools {
		reg.Register(tool)
	}

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

func TestE2EHeadlessToolCall(t *testing.T) {
	cfg := loadE2EConfig(t)
	fe := &recordingFrontend{}

	// Create a temp file with known content.
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello-gohome"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	prompt := fmt.Sprintf("Read the file at %s and tell me its exact content. Reply with just the content, nothing else.", testFile)
	history := []common.Message{
		{
			Role:    common.RoleUser,
			Content: []common.Block{{Kind: common.BlockText, Text: prompt}},
		},
	}

	a, sess := newE2EAgent(t, cfg, fe, history)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	if err := a.Run(ctx, sess); err != nil {
		t.Fatalf("agent.Run: %v", err)
	}

	// Verify a tool was called.
	fe.mu.Lock()
	var sawToolResult bool
	for _, ev := range fe.events {
		if ev.Kind == agent.EventToolResult {
			sawToolResult = true
		}
	}
	fe.mu.Unlock()
	if !sawToolResult {
		t.Error("expected at least one EventToolResult (tool call)")
	}

	// Verify the assistant response contains the file content.
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
	if !strings.Contains(lastAssistantText, "hello-gohome") {
		t.Errorf("assistant reply should contain 'hello-gohome', got: %q", lastAssistantText)
	}
	t.Logf("assistant replied: %q", lastAssistantText)
}

func TestE2EHeadlessMultiTurn(t *testing.T) {
	cfg := loadE2EConfig(t)

	// Prepare JSONL input: two user messages then exit.
	input := strings.Join([]string{
		`{"type":"user_message","content":"What is 2+2? Reply with just the number."}`,
		`{"type":"exit"}`,
	}, "\n") + "\n"

	var output strings.Builder
	hfe := headless.NewInteractiveFrontend(
		strings.NewReader(input),
		true,
		&output,
	)

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

	wl, werr := guard.Compile(guard.WhitelistFile{}, guard.WhitelistFile{}, "")
	if werr != nil {
		t.Fatalf("guard.Compile: %v", werr)
	}
	g := guard.NewGuard(wl, hfe, nil)
	g.SetYolo(true)

	tmpDir := t.TempDir()
	writerPath := filepath.Join(tmpDir, "e2e-multi.jsonl")
	w, err := session.OpenWriter(writerPath)
	if err != nil {
		t.Fatalf("session.OpenWriter: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	sess := session.NewSession("e2e-multi", tmpDir, cfg.model, string(cfg.wire))

	state := agent.NewSessionState(sess, w, client)
	a := &agent.Agent{
		Tools:     reg,
		Guard:     g,
		Frontend:  hfe,
		State:     state,
		System:    "You are a helpful assistant.",
		MaxTokens: 64,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Run the interactive loop: read input, run agent, repeat until exit.
	for {
		userText, inputErr := hfe.AwaitUserInput(ctx)
		if inputErr != nil {
			if errors.Is(inputErr, headless.ErrExit) {
				break
			}
			t.Fatalf("AwaitUserInput: %v", inputErr)
		}

		sess.History = append(sess.History, common.Message{
			Role:    common.RoleUser,
			Content: []common.Block{{Kind: common.BlockText, Text: userText}},
		})

		if runErr := a.Run(ctx, sess); runErr != nil {
			t.Fatalf("agent.Run: %v", runErr)
		}
	}

	// Verify the agent responded with something containing "4".
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
	if !strings.Contains(lastAssistantText, "4") {
		t.Errorf("assistant should have responded with '4', got: %q", lastAssistantText)
	}

	// Verify verbose output was written (JSONL events on stdout).
	if output.Len() == 0 {
		t.Error("expected verbose JSONL output, got nothing")
	}
	t.Logf("assistant replied: %q", lastAssistantText)
	t.Logf("output length: %d bytes", output.Len())
}

func TestE2EAutoCompact(t *testing.T) {
	cfg := loadE2EConfig(t)
	fe := &recordingFrontend{}

	// Seed with a long conversation history to trigger compaction.
	var history []common.Message
	for i := 0; i < 10; i++ {
		history = append(history,
			common.Message{
				Role:    common.RoleUser,
				Content: []common.Block{{Kind: common.BlockText, Text: fmt.Sprintf("Tell me about topic %d in detail.", i)}},
			},
			common.Message{
				Role:    common.RoleAssistant,
				Content: []common.Block{{Kind: common.BlockText, Text: strings.Repeat(fmt.Sprintf("Here is a detailed explanation about topic %d. ", i), 50)}},
			},
		)
	}
	// Final prompt.
	history = append(history, common.Message{
		Role:    common.RoleUser,
		Content: []common.Block{{Kind: common.BlockText, Text: "Summarize everything we discussed. Reply briefly."}},
	})

	a, sess := newE2EAgent(t, cfg, fe, history)

	// Enable auto-compact with a very low trigger threshold.
	a.CompactCfg = agent.CompactConfig{
		Enabled:       true,
		Mode:          "percentage",
		TriggerPct:    0.01, // trigger almost immediately
		ContextWindow: 128000,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	if err := a.Run(ctx, sess); err != nil {
		t.Fatalf("agent.Run: %v", err)
	}

	// Verify compaction fired.
	fe.mu.Lock()
	var sawCompacted bool
	for _, ev := range fe.events {
		if ev.Kind == agent.EventCompacted {
			sawCompacted = true
			t.Logf("compaction: %d -> %d tokens", ev.CompactBefore, ev.CompactAfter)
		}
	}
	fe.mu.Unlock()
	if !sawCompacted {
		t.Error("expected EventCompacted but it was not emitted")
	}

	// Verify the agent still produced a response after compaction.
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
		t.Error("no assistant response after compaction")
	}
	t.Logf("post-compaction reply length: %d chars", len(lastAssistantText))
}
