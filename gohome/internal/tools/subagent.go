package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// SubagentSpawner is implemented by Agent to spawn an isolated child agent.
// The spawner is responsible for creating the child session, wiring up a writer,
// running the child, and returning the text of the child's final assistant message.
type SubagentSpawner interface {
	Spawn(ctx context.Context, task, systemPrompt string) (resultText string, isError bool, err error)
}

// subagentTool implements the "subagent" tool.
type subagentTool struct {
	spawner SubagentSpawner
}

// NewSubagentTool returns a Tool that spawns an isolated child agent via spawner.
func NewSubagentTool(spawner SubagentSpawner) Tool {
	return &subagentTool{spawner: spawner}
}

func (t *subagentTool) Name() string { return "subagent" }

func (t *subagentTool) Description() string {
	return "Spawn an isolated subagent to execute a well-scoped task. " +
		"The subagent runs sequentially, has access to the same tools (except subagent itself), " +
		"and its result is the text of its final assistant message. " +
		"Use for tasks that benefit from a clean context and focused system prompt."
}

var subagentSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "task":          {"type": "string", "description": "The task to hand off to the subagent."},
    "system_prompt": {"type": "string", "description": "Optional system prompt for the subagent. If omitted, the parent system prompt is inherited."}
  },
  "required": ["task"]
}`)

func (t *subagentTool) InputSchema() json.RawMessage { return subagentSchema }

type subagentInput struct {
	Task         string `json:"task"`
	SystemPrompt string `json:"system_prompt"`
}

func (t *subagentTool) Execute(ctx context.Context, in json.RawMessage, _ ProgressSink) (Result, error) {
	var inp subagentInput
	if err := json.Unmarshal(in, &inp); err != nil {
		return Result{IsError: true, Content: fmt.Sprintf("subagent: invalid input: %v", err)}, nil
	}
	if inp.Task == "" {
		return Result{IsError: true, Content: "subagent: task must not be empty"}, nil
	}

	resultText, isError, err := t.spawner.Spawn(ctx, inp.Task, inp.SystemPrompt)
	if err != nil {
		return Result{IsError: true, Content: fmt.Sprintf("subagent: spawn error: %v", err)}, nil
	}
	return Result{Content: resultText, IsError: isError}, nil
}
