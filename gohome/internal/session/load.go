package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// Load reads a JSONL session file and reconstructs the Session metadata and
// conversation history.
//
// Event types handled:
//   - session_start  -> Session metadata (id, cwd, model, modelConfig, depth, parentId, startedAt)
//   - model_change   -> replaces the session's model and modelConfig
//   - user_message   -> common.Message{Role: RoleUser}
//   - assistant_message -> common.Message{Role: RoleAssistant}
//   - tool_result    -> BlockToolResult; consecutive results are merged into
//     one common.Message{Role: RoleTool}, matching the live agent loop
//   - compaction     -> replaces the history so far
//
// Events approval, subagent_spawn, subagent_done, session_end are ignored for
// history reconstruction. subagent_spawn events are counted so new child IDs
// continue the numbering. After replay, any tool_use without a recorded
// result gets a synthetic error result so the history is valid to resend.
func Load(path string) (*Session, []common.Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("session: load %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var sess *Session
	var history []common.Message
	spawned := 0

	err = forEachLine(f, func(raw []byte) {
		line := string(raw)

		var envelope struct {
			Type string `json:"type"`
			TS   string `json:"ts"`
		}
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			return
		}

		switch envelope.Type {
		case "session_start":
			var ev struct {
				ID          string    `json:"id"`
				ParentID    string    `json:"parentId"`
				CWD         string    `json:"cwd"`
				Model       string    `json:"model"`
				ModelConfig string    `json:"modelConfig"`
				Depth       int       `json:"depth"`
				StartedAt   time.Time `json:"startedAt"`
				TS          string    `json:"ts"`
			}
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				return
			}
			sess = NewSession(ev.ID, ev.CWD, ev.Model, ev.ModelConfig)
			sess.ParentID = ev.ParentID
			sess.Depth = ev.Depth
			// Prefer explicit StartedAt field; fall back to envelope ts.
			if !ev.StartedAt.IsZero() {
				sess.StartedAt = ev.StartedAt
			} else if t, err := time.Parse(time.RFC3339, ev.TS); err == nil {
				sess.StartedAt = t
			}

		case "model_change":
			var ev ModelChange
			if sess == nil || json.Unmarshal(raw, &ev) != nil {
				return
			}
			sess.Model = ev.Model
			sess.ModelConfig = ev.ModelConfig

		case "user_message":
			var ev struct {
				Content []common.Block `json:"content"`
			}
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				return
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
				return
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
				return
			}
			blk := common.Block{
				Kind:       common.BlockToolResult,
				ToolUseID:  ev.ToolUseID,
				ResultText: ev.Content,
				IsError:    ev.IsError,
			}
			// The live loop sends all results of one assistant turn as a
			// single RoleTool message, so merge consecutive results.
			if n := len(history); n > 0 && history[n-1].Role == common.RoleTool {
				history[n-1].Content = append(history[n-1].Content, blk)
			} else {
				history = append(history, common.Message{
					Role:    common.RoleTool,
					Content: []common.Block{blk},
				})
			}

		case "subagent_spawn":
			spawned++

		case "compaction":
			var ev struct {
				Summary string           `json:"summary"`
				History []common.Message `json:"history"`
			}
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				return
			}
			if len(ev.History) > 0 {
				history = make([]common.Message, len(ev.History))
				copy(history, ev.History)
			} else {
				history = []common.Message{
					{
						Role: common.RoleUser,
						Content: []common.Block{
							{Kind: common.BlockText, Text: CompactSummaryPrefix + ev.Summary},
						},
					},
				}
			}

		}
	})
	if err != nil {
		return nil, nil, fmt.Errorf("session: scan %s: %w", path, err)
	}
	if sess == nil {
		return nil, nil, fmt.Errorf("session: no session_start found in %s", path)
	}

	history = repairToolResults(slog.Default(), sess.ID, history)
	sess.History = history
	sess.subagents = spawned
	validateHistory(slog.Default(), sess.ID, history)
	return sess, history, nil
}

// forEachLine calls fn for every non-empty line in r. Unlike bufio.Scanner it
// has no line length limit, so large tool results and compaction events are
// read intact. The slice passed to fn is only valid during the call.
func forEachLine(r io.Reader, fn func(line []byte)) error {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if trimmed := bytes.TrimRight(line, "\r\n"); len(trimmed) > 0 {
			fn(trimmed)
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
