package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// Run drives the full agentic loop: it repeatedly calls Turn and dispatches
// tool calls until the LLM stops requesting tools or the context is cancelled.
//
// Session context is injected into ctx so that tools can access session state.
func (a *Agent) Run(ctx context.Context, sess *session.Session) error {
	// Record the active session so Spawn can reference it.
	a.Session = sess

	// Inject session into ctx so tools can call tools.SessionFrom(ctx).
	tctx := tools.WithSession(ctx, sess)

	for {
		stopReason, err := a.Turn(tctx, sess)
		if err != nil {
			if ctx.Err() != nil {
				// Context was cancelled during Turn.
				a.Frontend.Emit(sess.ID, Event{
					Kind:       EventTurnDone,
					SessionID:  sess.ID,
					StopReason: "cancelled",
				})
				if a.Writer != nil {
					a.Writer.Emit(session.SessionEnd{Reason: "cancelled"})
				}
				return ctx.Err()
			}
			return err
		}

		// Find the last assistant message.
		var toolUseBlocks []common.Block
		if len(sess.History) > 0 {
			last := sess.History[len(sess.History)-1]
			if last.Role == common.RoleAssistant {
				for _, b := range last.Content {
					if b.Kind == common.BlockToolUse {
						toolUseBlocks = append(toolUseBlocks, b)
					}
				}
			}
		}

		// No tool calls: the loop is done.
		if len(toolUseBlocks) == 0 {
			return nil
		}

		// Dispatch each tool call and collect results.
		var resultBlocks []common.Block
		for _, block := range toolUseBlocks {
			content, isError := a.dispatchTool(ctx, tctx, sess, block)

			// Persist the tool result event.
			if a.Writer != nil {
				a.Writer.Emit(session.ToolResult{
					ToolUseID: block.ToolUseID,
					Content:   content,
					IsError:   isError,
				})
			}

			// Forward to Frontend.
			a.Frontend.Emit(sess.ID, Event{
				Kind:       EventToolResult,
				SessionID:  sess.ID,
				ToolCallID: block.ToolUseID,
				Result: &ToolResult{
					ToolUseID: block.ToolUseID,
					Content:   content,
					IsError:   isError,
				},
			})

			resultBlocks = append(resultBlocks, common.Block{
				Kind:       common.BlockToolResult,
				ToolUseID:  block.ToolUseID,
				ResultText: content,
				IsError:    isError,
			})
		}

		// Append all results as a single RoleTool message.
		sess.History = append(sess.History, common.Message{
			Role:    common.RoleTool,
			Content: resultBlocks,
		})

		_ = stopReason // used implicitly: we continue the loop when toolUseBlocks non-empty
	}
}

// dispatchTool runs guard.Check, persists an Approval event, and either
// executes the tool or synthesises a denial result.
//
// It returns (content, isError).
func (a *Agent) dispatchTool(
	ctx context.Context,
	tctx context.Context,
	sess *session.Session,
	block common.Block,
) (content string, isError bool) {
	input := json.RawMessage(block.InputJSON)

	// Guard check.
	dec, err := a.Guard.Check(ctx, sess.ID, block.ToolName, input)
	if err != nil {
		return fmt.Sprintf("guard error: %v", err), true
	}

	// Persist approval event.
	if a.Writer != nil {
		a.Writer.Emit(session.Approval{
			ToolUseID:    block.ToolUseID,
			Outcome:      dec.Reason,
			SavedPattern: dec.SavedPattern,
			SteerMessage: dec.SteerMessage,
		})
	}

	if !dec.Allow {
		// Denied. Use steer message if available.
		if dec.SteerMessage != "" {
			return dec.SteerMessage, true
		}
		return "Tool call denied by user.", true
	}

	// Allowed: look up and execute.
	tool, ok := a.Tools.Get(block.ToolName)
	if !ok {
		return fmt.Sprintf("unknown tool: %s", block.ToolName), true
	}

	res, _ := safeExecute(tctx, tool, input, tools.NullSink{})
	return res.Content, res.IsError
}

// safeExecute calls tool.Execute and recovers from any panic, returning an
// IsError result rather than crashing the agent loop.
func safeExecute(
	ctx context.Context,
	tool tools.Tool,
	input json.RawMessage,
	sink tools.ProgressSink,
) (result tools.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("tool panicked",
				"tool", tool.Name(),
				"panic", r,
				"stack", string(debug.Stack()),
			)
			result = tools.Result{
				IsError: true,
				Content: fmt.Sprintf("tool panicked: %v", r),
			}
			err = nil
		}
	}()
	return tool.Execute(ctx, input, sink)
}
