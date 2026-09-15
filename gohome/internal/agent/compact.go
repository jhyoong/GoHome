package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

// CompactConfig controls automatic context compaction.
type CompactConfig struct {
	Enabled       bool
	Mode          string  // "percentage" or "leftover"
	TriggerPct    float64 // fraction (0..1) of context window that triggers compaction
	TargetPct     float64 // unused for now; reserved for future partial compaction
	Leftover      int     // minimum remaining tokens before triggering (leftover mode)
	ContextWindow int     // total context window size in tokens
}

// shouldCompact returns true when the given usage indicates the context
// is full enough to warrant compaction.
func (cfg CompactConfig) shouldCompact(usage common.Usage) bool {
	if !cfg.Enabled || cfg.ContextWindow <= 0 {
		return false
	}
	used := usage.InputTokens + usage.OutputTokens
	switch cfg.Mode {
	case "percentage":
		return float64(used)/float64(cfg.ContextWindow) >= cfg.TriggerPct
	case "leftover":
		return (cfg.ContextWindow - used) < cfg.Leftover
	}
	return false
}

const minCompactMessages = 4

// compact sends older conversation history to the LLM for summarization,
// keeps the first 2 messages (stable prefix for prompt caching) and the
// last ~4 messages (recent turns) intact, summarizes everything in between,
// and rebuilds History as: stablePrefix + summary + recentMessages.
// Persists a Compaction event and emits an EventCompacted to the frontend.
func (a *Agent) compact(ctx context.Context, sess *session.Session) error {
	if len(sess.History) < minCompactMessages {
		return nil
	}

	prompt := a.CompactPrompt
	if prompt == "" {
		prompt = defaultCompactPrompt
	}

	keepCount := 4
	prefixCount := 2

	// If the last message of the prefix is an assistant with tool_use blocks,
	// shrink the prefix so it doesn't orphan the tool_use.
	if prefixCount > 0 && prefixCount < len(sess.History) {
		last := sess.History[prefixCount-1]
		if last.Role == common.RoleAssistant {
			hasToolUse := false
			for _, b := range last.Content {
				if b.Kind == common.BlockToolUse {
					hasToolUse = true
					break
				}
			}
			if hasToolUse {
				prefixCount = 0
			}
		}
	}

	// Need at least: prefix + 1 message to summarize + keepCount recent
	minRequired := prefixCount + 1 + keepCount
	if len(sess.History) < minRequired {
		return nil
	}

	splitIdx := len(sess.History) - keepCount

	// Don't split a tool-use/tool-result pair: if splitIdx lands on a
	// RoleTool message, include its preceding assistant message too.
	if splitIdx > 0 && sess.History[splitIdx].Role == common.RoleTool {
		splitIdx--
	}

	// If oldMessages would start with RoleTool, push the split forward.
	if prefixCount < len(sess.History) && sess.History[prefixCount].Role == common.RoleTool {
		prefixCount++
	}

	if splitIdx <= prefixCount {
		return nil
	}

	stablePrefix := make([]common.Message, prefixCount)
	copy(stablePrefix, sess.History[:prefixCount])

	oldMessages := sess.History[prefixCount:splitIdx]
	recentMessages := make([]common.Message, len(sess.History[splitIdx:]))
	copy(recentMessages, sess.History[splitIdx:])

	beforeTokens := 0
	for _, msg := range oldMessages {
		for _, b := range msg.Content {
			beforeTokens += len(b.Text) / 4
			beforeTokens += len(b.InputJSON) / 4
			beforeTokens += len(b.ResultText) / 4
		}
	}

	maxTokens := 4096
	if a.MaxTokens > 0 {
		maxTokens = a.MaxTokens
	}

	req := common.Request{
		Model:     sess.Model,
		System:    prompt,
		Messages:  stripToolBlocks(oldMessages),
		MaxTokens: maxTokens,
	}

	events, err := a.State.Client().Stream(ctx, req)
	if err != nil {
		return err
	}

	var sb strings.Builder
	for ev := range events {
		switch ev.Kind {
		case common.EventTextDelta:
			sb.WriteString(ev.TextDelta)
		case common.EventError:
			return ev.Err
		}
	}
	summary := sb.String()

	if summary == "" {
		slog.Warn("compact: LLM returned empty summary, skipping")
		return nil
	}

	summaryMsg := common.Message{
		Role: common.RoleUser,
		Content: []common.Block{
			{Kind: common.BlockText, Text: session.CompactSummaryPrefix + summary},
		},
	}

	sess.History = make([]common.Message, 0, len(stablePrefix)+1+len(recentMessages))
	sess.History = append(sess.History, stablePrefix...)
	sess.History = append(sess.History, summaryMsg)
	sess.History = append(sess.History, recentMessages...)

	afterTokens := len(summary) / 4

	if w := a.State.Writer(); w != nil {
		w.Emit(session.Compaction{
			BeforeTokens: beforeTokens,
			AfterTokens:  afterTokens,
			Summary:      summary,
		})
	}

	a.Frontend.Emit(sess.ID, Event{
		Kind:          EventCompacted,
		SessionID:     sess.ID,
		CompactBefore: beforeTokens,
		CompactAfter:  afterTokens,
	})

	return nil
}

// stripToolBlocks converts tool_use and tool_result blocks to plain text,
// and converts RoleTool messages to RoleUser, so the summarization request
// can be sent without a tools definition.
func stripToolBlocks(msgs []common.Message) []common.Message {
	out := make([]common.Message, 0, len(msgs))
	for _, msg := range msgs {
		role := msg.Role
		if role == common.RoleTool {
			role = common.RoleUser
		}
		var blocks []common.Block
		for _, b := range msg.Content {
			switch b.Kind {
			case common.BlockToolUse:
				blocks = append(blocks, common.Block{
					Kind: common.BlockText,
					Text: fmt.Sprintf("[Tool call: %s(%s)]", b.ToolName, b.InputJSON),
				})
			case common.BlockToolResult:
				blocks = append(blocks, common.Block{
					Kind: common.BlockText,
					Text: fmt.Sprintf("[Result: %s]", b.ResultText),
				})
			default:
				blocks = append(blocks, b)
			}
		}
		out = append(out, common.Message{Role: role, Content: blocks})
	}
	return out
}

const defaultCompactPrompt = `You are summarizing a coding assistant conversation for context compaction.
Produce a concise summary that preserves:
- The user's current goal and any sub-tasks
- Key decisions made and their reasoning
- File paths and code changes discussed or made
- Any pending work or unresolved issues
- Tool results that are still relevant

Be factual and specific. Do not add commentary or analysis.
Write the summary as a narrative, not a bulleted list.`
