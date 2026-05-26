package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jhyoong/gohome/internal/approval"
	"github.com/jhyoong/gohome/internal/llm"
	"github.com/jhyoong/gohome/internal/session"
	"github.com/jhyoong/gohome/internal/tools"
)

// SubagentEvents receives streaming events from a running subagent.
// Implemented by the server per WebSocket connection.
type SubagentEvents interface {
	OnStart(sessionID, parentID, task string)
	OnToken(sessionID, token string)
	OnThinkingToken(sessionID, token string)
	OnToolResult(sessionID, tool, params, result string, approved bool)
	OnDone(sessionID, finalText string)
	OnError(sessionID, errMsg string)
}

// SpawnSubagentTool is a Tool that runs a subagent loop for a delegated task.
// It must be constructed per-run (not shared across connections) because it
// captures the parent session ID and the per-connection broker and events.
type SpawnSubagentTool struct {
	llm          *llm.Client
	registry     *tools.Registry
	store        *session.Store
	broker       *approval.Broker
	events       SubagentEvents
	systemPrompt string
	parentID     string
}

func NewSpawnSubagentTool(
	client *llm.Client,
	reg *tools.Registry,
	store *session.Store,
	broker *approval.Broker,
	events SubagentEvents,
	systemPrompt string,
	parentID string,
) *SpawnSubagentTool {
	return &SpawnSubagentTool{
		llm:          client,
		registry:     reg,
		store:        store,
		broker:       broker,
		events:       events,
		systemPrompt: systemPrompt,
		parentID:     parentID,
	}
}

func (t *SpawnSubagentTool) Name() string { return "spawn_subagent" }

func (t *SpawnSubagentTool) Description() string {
	return "Spawn a subagent to handle a delegated task. The subagent runs independently and returns its final response. Use this to delegate work and reduce context pressure."
}

func (t *SpawnSubagentTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"task": {
				"type": "string",
				"description": "A complete, self-contained description of the task for the subagent to complete"
			}
		},
		"required": ["task"]
	}`)
}

func (t *SpawnSubagentTool) Execute(ctx context.Context, params json.RawMessage) (string, error) {
	var p struct {
		Task string `json:"task"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return "", fmt.Errorf("invalid params: %w", err)
	}
	if p.Task == "" {
		return "", fmt.Errorf("task is required")
	}

	child, err := t.store.CreateChildSession(ctx, t.parentID)
	if err != nil {
		return "", fmt.Errorf("creating child session: %w", err)
	}

	t.events.OnStart(child.ID, t.parentID, p.Task)

	var finalText string
	loop := NewLoop(t.llm, t.registry, t.store, t.systemPrompt)
	err = loop.Run(ctx, child.ID, "", p.Task, t.broker,
		func(token string) {
			finalText += token
			t.events.OnToken(child.ID, token)
		},
		func(errMsg string) {
			t.events.OnError(child.ID, errMsg)
		},
		func(tool, toolParams, result string, approved bool) {
			t.events.OnToolResult(child.ID, tool, toolParams, result, approved)
		},
		nil, // subagents do not support steering
		nil, // usage not forwarded to main session
		func(token string) {
			t.events.OnThinkingToken(child.ID, token)
		},
	)
	// The onError callback above handles non-fatal streaming errors during the
	// loop. The returned err here is a fatal loop termination error. loop.Run
	// never calls onError before returning a non-nil error, so these two paths
	// do not double-fire.
	if err != nil {
		t.events.OnError(child.ID, err.Error())
		return "", err
	}

	t.events.OnDone(child.ID, finalText)
	return finalText, nil
}
