package session

import (
	"log/slog"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
)

// validateHistory checks loaded session messages for malformed thinking
// blocks (no text and no signature).
// It logs warnings but does not modify or remove any messages.
func validateHistory(logger *slog.Logger, sessionID string, msgs []common.Message) {
	for i, msg := range msgs {
		for j, b := range msg.Content {
			if b.Kind != common.BlockThinking {
				continue
			}
			// A block with no text but a signature is valid: current Claude
			// models omit thinking text by default. Redacted blocks are
			// skipped above.
			if b.Text == "" && b.Signature == "" {
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
// the API on resume. Results are put in tool_use order.
//
// Only the message right after each assistant turn is checked. That is
// enough: the agent writes a turn's results right after the assistant
// message, and the events written in between (approval, subagent markers)
// do not add messages on Load. See
// TestLoad_NonHistoryEventsBetweenToolUseAndResults.
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

		var recorded []common.Block
		if i+1 < len(msgs) && msgs[i+1].Role == common.RoleTool {
			recorded = msgs[i+1].Content
			i++
		}

		// Rebuild the results in tool_use order, filling gaps with synthetic
		// results. A new slice, so the caller's messages are not modified.
		first := make(map[string]int, len(recorded))
		for j, b := range recorded {
			if _, seen := first[b.ToolUseID]; !seen && b.Kind == common.BlockToolResult {
				first[b.ToolUseID] = j
			}
		}
		used := make([]bool, len(recorded))
		content := make([]common.Block, 0, len(useIDs))
		for _, id := range useIDs {
			if j, ok := first[id]; ok {
				content = append(content, recorded[j])
				used[j] = true
				continue
			}
			logger.Warn("tool_use without result, adding synthetic result",
				"session", sessionID,
				"tool_use_id", id,
			)
			content = append(content, common.Block{
				Kind:       common.BlockToolResult,
				ToolUseID:  id,
				ResultText: skippedResultText,
				IsError:    true,
			})
		}
		// Keep any other recorded blocks after the results.
		for j, b := range recorded {
			if !used[j] {
				content = append(content, b)
			}
		}
		out = append(out, common.Message{Role: common.RoleTool, Content: content})
	}
	return out
}
