package agent

import (
	"context"
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

// compact sends the current conversation history to the LLM for
// summarization, replaces the history with the summary, persists a
// Compaction event, and emits an EventCompacted to the frontend.
func (a *Agent) compact(ctx context.Context, sess *session.Session) error {
	if len(sess.History) == 0 {
		return nil
	}

	prompt := a.CompactPrompt
	if prompt == "" {
		prompt = defaultCompactPrompt
	}

	beforeTokens := 0
	for _, msg := range sess.History {
		for _, b := range msg.Content {
			beforeTokens += len(b.Text) / 4
			beforeTokens += len(b.InputJSON) / 4
			beforeTokens += len(b.ResultText) / 4
		}
	}

	req := common.Request{
		Model:     sess.Model,
		System:    prompt,
		Messages:  sess.History,
		MaxTokens: a.MaxTokens,
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

	sess.History = []common.Message{
		{
			Role: common.RoleUser,
			Content: []common.Block{
				{Kind: common.BlockText, Text: session.CompactSummaryPrefix + summary},
			},
		},
	}

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

const defaultCompactPrompt = `You are summarizing a coding assistant conversation for context compaction.
Produce a concise summary that preserves:
- The user's current goal and any sub-tasks
- Key decisions made and their reasoning
- File paths and code changes discussed or made
- Any pending work or unresolved issues
- Tool results that are still relevant

Be factual and specific. Do not add commentary or analysis.
Write the summary as a narrative, not a bulleted list.`
