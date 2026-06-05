package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// Load reads a JSONL session file and reconstructs the Session metadata and
// conversation history.
//
// Event types handled:
//   - session_start  -> Session metadata (id, cwd, model, endpoint, depth, parentId, startedAt)
//   - user_message   -> common.Message{Role: RoleUser}
//   - assistant_message -> common.Message{Role: RoleAssistant}
//   - tool_result    -> common.Message{Role: RoleTool, single BlockToolResult}
//
// Events approval, subagent_spawn, subagent_done, session_end are ignored for
// history reconstruction.
func Load(path string) (*Session, []common.Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("session: load %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var sess *Session
	var history []common.Message

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var envelope struct {
			Type string `json:"type"`
			TS   string `json:"ts"`
		}
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			continue
		}

		switch envelope.Type {
		case "session_start":
			var ev struct {
				ID        string    `json:"id"`
				ParentID  string    `json:"parentId"`
				CWD       string    `json:"cwd"`
				Model     string    `json:"model"`
				Endpoint  string    `json:"endpoint"`
				Depth     int       `json:"depth"`
				StartedAt time.Time `json:"startedAt"`
				TS        string    `json:"ts"`
			}
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				continue
			}
			sess = NewSession(ev.ID, ev.CWD, ev.Model, ev.Endpoint)
			sess.ParentID = ev.ParentID
			sess.Depth = ev.Depth
			// Prefer explicit StartedAt field; fall back to envelope ts.
			if !ev.StartedAt.IsZero() {
				sess.StartedAt = ev.StartedAt
			} else if t, err := time.Parse(time.RFC3339, ev.TS); err == nil {
				sess.StartedAt = t
			}

		case "user_message":
			var ev struct {
				Content []common.Block `json:"content"`
			}
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				continue
			}
			history = append(history, common.Message{
				Role:    common.RoleUser,
				Content: ev.Content,
			})

		case "assistant_message":
			var ev struct {
				Content []common.Block `json:"content"`
			}
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				continue
			}
			history = append(history, common.Message{
				Role:    common.RoleAssistant,
				Content: ev.Content,
			})

		case "tool_result":
			var ev struct {
				ToolUseID string `json:"toolUseId"`
				Content   string `json:"content"`
				IsError   bool   `json:"isError"`
			}
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				continue
			}
			history = append(history, common.Message{
				Role: common.RoleTool,
				Content: []common.Block{
					{
						Kind:       common.BlockToolResult,
						ToolUseID:  ev.ToolUseID,
						ResultText: ev.Content,
						IsError:    ev.IsError,
					},
				},
			})

			// Ignored for history reconstruction:
			// approval, subagent_spawn, subagent_done, session_end
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("session: scan %s: %w", path, err)
	}
	if sess == nil {
		return nil, nil, fmt.Errorf("session: no session_start found in %s", path)
	}

	sess.History = history
	return sess, history, nil
}
