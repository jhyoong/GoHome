package agent

import (
	"context"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

// Turn executes one LLM request/response cycle, consuming all streaming
// events until the turn ends (EventTurnDone) or an error occurs.
//
// It mutates sess.History by appending the assistant message, persists an
// AssistantMessage to a.Writer (if non-nil), and forwards events to a.Frontend.
//
// Returns the stopReason string (e.g. "end_turn", "tool_use") or an error if
// the stream failed. A cancelled context causes the event loop to stop early;
// Run is responsible for wrapping that into the appropriate events/errors.
func (a *Agent) Turn(ctx context.Context, sess *session.Session) (string, error) {
	req := common.Request{
		Model:     sess.Model,
		System:    a.System,
		Messages:  sess.History,
		Tools:     a.Tools.Schemas(),
		MaxTokens: 4096,
	}

	events, err := a.Client.Stream(ctx, req)
	if err != nil {
		return "", err
	}

	var (
		textBuf    string
		toolBlocks []common.Block // tool_use blocks in arrival order
		stopReason string
		usage      *common.Usage
	)

	for {
		select {
		case <-ctx.Done():
			// Cancelled mid-stream. Caller (Run) handles this.
			return "", ctx.Err()

		case ev, ok := <-events:
			if !ok {
				// Channel closed without EventTurnDone — treat as end_turn.
				goto done
			}

			switch ev.Kind {
			case common.EventTextDelta:
				textBuf += ev.TextDelta
				a.Frontend.Emit(sess.ID, Event{
					Kind:      EventTokenDelta,
					SessionID: sess.ID,
					TextDelta: ev.TextDelta,
				})

			case common.EventToolCallDone:
				toolBlocks = append(toolBlocks, common.Block{
					Kind:      common.BlockToolUse,
					ToolUseID: ev.ToolCallID,
					ToolName:  ev.ToolName,
					InputJSON: ev.InputJSON,
				})
				a.Frontend.Emit(sess.ID, Event{
					Kind:       EventToolCallDone,
					SessionID:  sess.ID,
					ToolCallID: ev.ToolCallID,
					ToolName:   ev.ToolName,
					InputJSON:  ev.InputJSON,
				})

			case common.EventTurnDone:
				stopReason = ev.StopReason
				usage = ev.Usage
				goto done

			case common.EventError:
				a.Frontend.Emit(sess.ID, Event{
					Kind:      EventError,
					SessionID: sess.ID,
					Err:       ev.Err,
				})
				return "", ev.Err
			}
		}
	}

done:
	// Build the assistant message: text block first, then tool_use blocks.
	var blocks []common.Block
	if textBuf != "" {
		blocks = append(blocks, common.Block{Kind: common.BlockText, Text: textBuf})
	}
	blocks = append(blocks, toolBlocks...)

	assistantMsg := common.Message{
		Role:    common.RoleAssistant,
		Content: blocks,
	}
	sess.History = append(sess.History, assistantMsg)

	// Persist to writer.
	if a.Writer != nil {
		a.Writer.Emit(session.AssistantMessage{
			Content:    blocks,
			StopReason: stopReason,
			Usage:      usage,
		})
	}

	// Forward usage and turn_done events.
	if usage != nil {
		a.Frontend.Emit(sess.ID, Event{
			Kind:      EventUsageUpdated,
			SessionID: sess.ID,
			Usage:     usage,
		})
	}
	a.Frontend.Emit(sess.ID, Event{
		Kind:       EventTurnDone,
		SessionID:  sess.ID,
		StopReason: stopReason,
	})

	return stopReason, nil
}
