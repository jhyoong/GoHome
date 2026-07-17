package session

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// Event variant structs — each maps to a JSONL event type.

type SessionStart struct {
	ID          string    `json:"id"`
	ParentID    string    `json:"parentId"`
	CWD         string    `json:"cwd"`
	Model       string    `json:"model"`
	ModelConfig string    `json:"modelConfig"`
	Depth       int       `json:"depth"`
	StartedAt   time.Time `json:"startedAt"`
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
	ChildID string `json:"childId"`
	Task    string `json:"task"`
}

type SubagentDone struct {
	ChildID string `json:"childId"`
	IsError bool   `json:"isError"`
}

type SessionEnd struct {
	Reason string `json:"reason"`
}

const CompactSummaryPrefix = "[Auto-compact summary]\n\n"

type Compaction struct {
	BeforeTokens int    `json:"beforeTokens"`
	AfterTokens  int    `json:"afterTokens"`
	Summary      string `json:"summary"`
}

// encode serialises ev as a flat single-line JSON object with "type" and "ts" fields.
// Returns an error for unknown event types.
func encode(ev any) ([]byte, error) {
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
	case Compaction:
		typeName = "compaction"
	default:
		return nil, fmt.Errorf("session: unknown event type %T", ev)
	}

	raw, err := json.Marshal(ev)
	if err != nil {
		return nil, fmt.Errorf("session: marshal event: %w", err)
	}

	ts := time.Now().UTC().Format(time.RFC3339)
	header := fmt.Sprintf(`{"type":%q,"ts":%q`, typeName, ts)
	if len(raw) > 2 {
		return append([]byte(header+","), raw[1:]...), nil
	}
	return []byte(header + "}"), nil
}
