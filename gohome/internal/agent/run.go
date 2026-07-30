package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// Run drives the full agentic loop: it repeatedly calls Turn and dispatches
// tool calls until the LLM stops requesting tools or the context is cancelled.
//
// Session context is injected into ctx so that tools can access session state.
func (a *Agent) Run(ctx context.Context, sess *session.Session) error {
	a.State.MarkBusy()
	defer a.State.MarkIdle()

	// Inject session into ctx so tools can call tools.SessionFrom(ctx).
	tctx := tools.WithSession(ctx, sess)

	for {
		a.Frontend.Emit(sess.ID, Event{
			Kind:      EventSending,
			SessionID: sess.ID,
		})
		_, usage, err := a.Turn(tctx, sess)
		if err != nil {
			if ctx.Err() != nil {
				// Context was cancelled during Turn. Emit a frontend event but
				// do NOT write session_end here — the writer's owner emits that.
				a.Frontend.Emit(sess.ID, Event{
					Kind:       EventTurnDone,
					SessionID:  sess.ID,
					StopReason: "cancelled",
				})
				return ctx.Err()
			}
			return err
		}

		if usage != nil && a.CompactCfg.shouldCompact(*usage) {
			if compactErr := a.compact(tctx, sess); compactErr != nil {
				slog.Warn("auto-compact failed, continuing with full history", "err", compactErr)
			}
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
		var anyDenied bool
		for _, block := range toolUseBlocks {
			content, isError, elapsed, denied := a.dispatchTool(ctx, tctx, sess, block)

			if denied {
				anyDenied = true
			}

			// Persist the tool result event.
			if w := a.State.Writer(); w != nil {
				w.Emit(session.ToolResult{
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
					Duration:  elapsed,
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

		if anyDenied {
			a.Frontend.Emit(sess.ID, Event{
				Kind:      EventToolDenied,
				SessionID: sess.ID,
			})
			return ErrToolDenied
		}
	}
}

// dispatchTool runs guard.Check, persists an Approval event, and either
// executes the tool or synthesises a denial result.
//
// It returns (content, isError, elapsed, denied). The denied flag is true only
// for plain denials (not steer), signalling that Run should return ErrToolDenied.
func (a *Agent) dispatchTool(
	ctx context.Context,
	tctx context.Context,
	sess *session.Session,
	block common.Block,
) (content string, isError bool, elapsed time.Duration, denied bool) {
	input := json.RawMessage(block.InputJSON)

	// Guard check.
	dec, err := a.Guard.Check(ctx, sess.ID, block.ToolName, input)
	if err != nil {
		return fmt.Sprintf("guard error: %v", err), true, 0, false
	}

	// Persist approval event.
	if w := a.State.Writer(); w != nil {
		w.Emit(session.Approval{
			ToolUseID:    block.ToolUseID,
			Outcome:      dec.Reason,
			SavedPattern: dec.SavedPattern,
			SteerMessage: dec.SteerMessage,
		})
	}

	if !dec.Allow {
		if dec.DenyInfo != "" {
			return dec.DenyInfo, true, 0, false
		}
		if dec.SteerMessage != "" {
			return dec.SteerMessage, true, 0, false
		}
		return "Tool call denied by user.", true, 0, true
	}

	// Allowed: look up and execute.
	tool, ok := a.Tools.Get(block.ToolName)
	if !ok {
		return fmt.Sprintf("unknown tool: %s", block.ToolName), true, 0, false
	}

	execCtx := tctx
	if dec.SudoPassword != "" {
		execCtx = tools.WithSudoPassword(tctx, dec.SudoPassword)
	}

	start := time.Now()
	res, execErr := safeExecute(execCtx, tool, input, tools.NullSink{})
	elapsed = time.Since(start)
	if execErr != nil {
		slog.Debug("tool execution error", "tool", block.ToolName, "err", execErr)
	}
	return res.Content, res.IsError, elapsed, false
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
