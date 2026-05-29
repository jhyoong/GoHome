package session

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// Event variant structs — each maps to a JSONL event type.

type SessionStart struct {
	ID        string    `json:"id"`
	ParentID  string    `json:"parentId"`
	CWD       string    `json:"cwd"`
	Model     string    `json:"model"`
	Endpoint  string    `json:"endpoint"`
	Depth     int       `json:"depth"`
	StartedAt time.Time `json:"startedAt"`
}

type UserMessage struct {
	Content []common.Block `json:"content"`
}

type AssistantMessage struct {
	Content    []common.Block `json:"content"`
	StopReason string         `json:"stopReason"`
	Usage      *common.Usage  `json:"usage,omitempty"`
}

type ToolResult struct {
	ToolUseID string `json:"toolUseId"`
	Content   string `json:"content"`
	IsError   bool   `json:"isError"`
}

type Approval struct {
	ToolUseID    string `json:"toolUseId"`
	Outcome      string `json:"outcome"`
	SavedPattern string `json:"savedPattern,omitempty"`
	SteerMessage string `json:"steerMessage,omitempty"`
}

type SubagentSpawn struct {
	ToolUseID string `json:"toolUseId"`
	ChildID   string `json:"childId"`
	Task      string `json:"task"`
}

type SubagentDone struct {
	ToolUseID string `json:"toolUseId"`
	ChildID   string `json:"childId"`
	IsError   bool   `json:"isError"`
}

type SessionEnd struct {
	Reason string `json:"reason"`
}

// Encode serialises ev as a flat single-line JSON object with "type" and "ts" fields.
// Returns an error for unknown event types.
func Encode(ev any) ([]byte, error) {
	var typeName string
	switch ev.(type) {
	case SessionStart:
		typeName = "session_start"
	case UserMessage:
		typeName = "user_message"
	case AssistantMessage:
		typeName = "assistant_message"
	case ToolResult:
		typeName = "tool_result"
	case Approval:
		typeName = "approval"
	case SubagentSpawn:
		typeName = "subagent_spawn"
	case SubagentDone:
		typeName = "subagent_done"
	case SessionEnd:
		typeName = "session_end"
	default:
		return nil, fmt.Errorf("session: unknown event type %T", ev)
	}

	// Marshal the struct to raw bytes, then unmarshal into a map so we can
	// inject the "type" and "ts" fields into the flat object.
	raw, err := json.Marshal(ev)
	if err != nil {
		return nil, fmt.Errorf("session: marshal event: %w", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("session: unmarshal into map: %w", err)
	}

	typeJSON, _ := json.Marshal(typeName)
	m["type"] = json.RawMessage(typeJSON)

	tsJSON, _ := json.Marshal(time.Now().UTC().Format(time.RFC3339))
	m["ts"] = json.RawMessage(tsJSON)

	out, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("session: marshal flat event: %w", err)
	}
	return out, nil
}
