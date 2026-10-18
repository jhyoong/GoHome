package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

// emitTurnError reports a turn failure to the frontend so the user sees it.
func (a *Agent) emitTurnError(sessionID string, err error) {
	a.Frontend.Emit(sessionID, Event{
		Kind:       EventError,
		SessionID:  sessionID,
		Err:        err,
		ErrMessage: err.Error(),
	})
}

// Turn executes one LLM request/response cycle, consuming all streaming
// events until the turn ends (EventTurnDone) or an error occurs.
//
// It mutates sess.History by appending the assistant message, persists an
// AssistantMessage to a.State.Writer() (if non-nil), and forwards events to a.Frontend.
//
// Returns the stopReason string (e.g. "end_turn", "tool_use") or an error if
// the stream failed. A cancelled context causes the event loop to stop early;
// Run is responsible for wrapping that into the appropriate events/errors.
func (a *Agent) Turn(ctx context.Context, sess *session.Session) (string, *common.Usage, error) {
	ms := a.State.ModelSettings()
	maxTokens := 4096
	if ms.MaxTokens > 0 {
		maxTokens = ms.MaxTokens
	}
	req := common.Request{
		Model:           ms.Name,
		System:          a.System,
		Messages:        sess.History,
		Tools:           a.Tools.Schemas(),
		MaxTokens:       maxTokens,
		Thinking:        ms.Thinking,
		ThinkingBudget:  ms.ThinkingBudget,
		ReasoningEffort: ms.ReasoningEffort,
	}

	start := time.Now()
	client := a.State.Client()
	if client == nil {
		err := ErrNoClient
		a.emitTurnError(sess.ID, err)
		return "", nil, err
	}
	var (
		// blocks holds content blocks in arrival order. Text and thinking
		// deltas extend the last block while it is still open.
		blocks     []common.Block
		open       bool // last block is a text or thinking block still receiving deltas
		stopReason string
		usage      *common.Usage
		// sent is set once anything for this attempt reached the frontend.
		sent   bool
		events <-chan common.StreamEvent
	)
	// appendDelta adds text to the open block of the given kind, or starts a
	// new block if the last block is closed or of another kind.
	// Empty deltas are ignored so no empty block is created.
	appendDelta := func(kind common.BlockKind, text string) {
		if text == "" {
			return
		}
		if open && blocks[len(blocks)-1].Kind == kind {
			blocks[len(blocks)-1].Text += text
			return
		}
		blocks = append(blocks, common.Block{Kind: kind, Text: text})
		open = true
	}

	backoff := a.RetryBackoff
	if backoff == nil {
		backoff = config.DefaultRetryBackoff
	}

retry:
	for attempt := 0; ; attempt++ {
		blocks, open, sent = nil, false, false
		var err error
		events, err = client.Stream(ctx, req)
		if err != nil {
			if ctx.Err() == nil {
				a.emitTurnError(sess.ID, err)
			}
			return "", nil, err
		}

		for {
			select {
			case <-ctx.Done():
				// Cancelled mid-stream. Caller (Run) handles this.
				return "", nil, ctx.Err()

			case ev, ok := <-events:
				if !ok {
					// Channel closed without EventTurnDone: the stream was cut off.
					if ctx.Err() != nil {
						return "", nil, ctx.Err()
					}
					err := common.ErrStreamEndedEarly
					a.emitTurnError(sess.ID, err)
					return "", nil, err
				}

				switch ev.Kind {
				case common.EventTextDelta:
					sent = true
					appendDelta(common.BlockText, ev.TextDelta)
					a.Frontend.Emit(sess.ID, Event{
						Kind:      EventTokenDelta,
						SessionID: sess.ID,
						TextDelta: ev.TextDelta,
					})

				case common.EventThinkingDelta:
					sent = true
					appendDelta(common.BlockThinking, ev.ThinkingDelta)
					a.Frontend.Emit(sess.ID, Event{
						Kind:          EventThinkingDelta,
						SessionID:     sess.ID,
						ThinkingDelta: ev.ThinkingDelta,
					})

				case common.EventThinkingDone:
					sent = true
					// Each thinking block keeps its own signature. A block with no
					// deltas but a signature (display "omitted") is kept; an empty
					// block with no signature (OpenAI) is skipped.
					if open && blocks[len(blocks)-1].Kind == common.BlockThinking {
						blocks[len(blocks)-1].Signature = ev.Signature
					} else if ev.Signature != "" {
						blocks = append(blocks, common.Block{Kind: common.BlockThinking, Signature: ev.Signature})
					}
					open = false
					a.Frontend.Emit(sess.ID, Event{
						Kind:      EventThinkingDone,
						SessionID: sess.ID,
					})

				case common.EventRedactedThinking:
					sent = true
					blocks = append(blocks, common.Block{Kind: common.BlockRedactedThinking, Data: ev.Data})
					open = false

				case common.EventToolCallDone:
					sent = true
					// Invalid input (e.g. cut off by max_tokens) is stored as {}
					// and marked so Run returns an error result instead of running it.
					input := ev.InputJSON
					invalid := input != "" && !json.Valid([]byte(input))
					if invalid {
						input = "{}"
					}
					open = false
					blocks = append(blocks, common.Block{
						Kind:         common.BlockToolUse,
						ToolUseID:    ev.ToolCallID,
						ToolName:     ev.ToolName,
						InputJSON:    input,
						InvalidInput: invalid,
					})
					a.Frontend.Emit(sess.ID, Event{
						Kind:       EventToolCallDone,
						SessionID:  sess.ID,
						ToolCallID: ev.ToolCallID,
						ToolName:   ev.ToolName,
						InputJSON:  input,
					})

				case common.EventTurnDone:
					stopReason = ev.StopReason
					usage = ev.Usage
					goto done

				case common.EventError:
					// An overloaded server before any content is retried.
					if errors.Is(ev.Err, common.ErrOverloaded) && !sent && attempt < len(backoff) {
						if err := common.Sleep(ctx, common.RetryDelay(backoff, attempt)); err != nil {
							return "", nil, err
						}
						continue retry
					}
					a.Frontend.Emit(sess.ID, Event{
						Kind:       EventError,
						SessionID:  sess.ID,
						Err:        ev.Err,
						ErrMessage: ev.Err.Error(),
					})
					return "", nil, ev.Err

				default:
					// Log unknown/future event kinds so they are visible rather than
					// silently dropped (e.g. EventToolCallPartial will land here).
					slog.Debug("agent: unhandled stream event", "kind", ev.Kind)
				}
			}
		}
	}

done:
	elapsed := time.Since(start)

	// Only persist and append when the assistant message has content.
	// Skipping an empty message prevents a malformed zero-content assistant
	// message from reaching wire adapters (which require at least one block).
	if len(blocks) > 0 {
		assistantMsg := common.Message{
			Role:       common.RoleAssistant,
			Content:    blocks,
			StopReason: stopReason,
		}
		sess.History = append(sess.History, assistantMsg)

		// Persist to writer.
		if w := a.State.Writer(); w != nil {
			w.Emit(session.AssistantMessage{
				Content:    blocks,
				StopReason: stopReason,
				Usage:      usage,
			})
		}
	}

	// Build turn stats from usage and elapsed time.
	var stats *TurnStats
	if usage != nil {
		stats = &TurnStats{
			OutputTokens:     usage.OutputTokens,
			InputTokens:      usage.InputTokens,
			CacheReadTokens:  usage.CacheReadTokens,
			CacheWriteTokens: usage.CacheWriteTokens,
			Elapsed:          elapsed,
		}
	}

	// Forward usage and turn_done events regardless of content — the Run loop
	// must always see EventTurnDone to know when to proceed.
	if usage != nil {
		a.Frontend.Emit(sess.ID, Event{
			Kind:      EventUsageUpdated,
			SessionID: sess.ID,
			Usage:     usage,
		})
	}
	if notice := common.TokenLimitNotice(stopReason); notice != "" {
		a.Frontend.Emit(sess.ID, Event{
			Kind:      EventNotice,
			SessionID: sess.ID,
			Notice:    notice,
		})
	}
	a.Frontend.Emit(sess.ID, Event{
		Kind:       EventTurnDone,
		SessionID:  sess.ID,
		StopReason: stopReason,
		TurnStats:  stats,
	})

	return stopReason, usage, nil
}
