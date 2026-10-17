package session

import (
	"log/slog"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// validateHistory checks loaded session messages for malformed thinking blocks.
// It logs warnings but does not modify or remove any messages.
func validateHistory(logger *slog.Logger, sessionID string, msgs []common.Message) {
	for i, msg := range msgs {
		for j, b := range msg.Content {
			if b.Kind != common.BlockThinking {
				continue
			}
			if b.Text == "" {
				logger.Warn("empty thinking block",
					"session", sessionID,
					"message", i,
					"block", j,
				)
			}
		}
	}
}

// skippedResultText is the content of synthetic results added by
// repairToolResults.
const skippedResultText = "skipped: no result recorded"

// repairToolResults makes sure every tool_use in an assistant message has a
// matching tool_result in the RoleTool message that follows it. Missing
// results are added as errors to that message, or to a new RoleTool message if
// none follows. Session files written before results were always recorded
// (for example when a steer skipped tool calls) would otherwise be rejected by
// the API on resume.
func repairToolResults(logger *slog.Logger, sessionID string, msgs []common.Message) []common.Message {
	out := make([]common.Message, 0, len(msgs))
	for i := 0; i < len(msgs); i++ {
		msg := msgs[i]
		out = append(out, msg)
		if msg.Role != common.RoleAssistant {
			continue
		}

		var useIDs []string
		for _, b := range msg.Content {
			if b.Kind == common.BlockToolUse {
				useIDs = append(useIDs, b.ToolUseID)
			}
		}
		if len(useIDs) == 0 {
			continue
		}

		var tool common.Message
		if i+1 < len(msgs) && msgs[i+1].Role == common.RoleTool {
			tool = msgs[i+1]
			// Copy so the caller's slice is not modified through shared storage.
			tool.Content = append([]common.Block(nil), tool.Content...)
			i++
		} else {
			tool = common.Message{Role: common.RoleTool}
		}

		have := make(map[string]bool, len(tool.Content))
		for _, b := range tool.Content {
			if b.Kind == common.BlockToolResult {
				have[b.ToolUseID] = true
			}
		}
		for _, id := range useIDs {
			if have[id] {
				continue
			}
			logger.Warn("tool_use without result, adding synthetic result",
				"session", sessionID,
				"tool_use_id", id,
			)
			tool.Content = append(tool.Content, common.Block{
				Kind:       common.BlockToolResult,
				ToolUseID:  id,
				ResultText: skippedResultText,
				IsError:    true,
			})
		}
		out = append(out, tool)
	}
	return out
}
