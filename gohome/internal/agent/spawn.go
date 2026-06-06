package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

// Spawn creates an isolated child agent for task, runs it to completion, and
// returns the text of the child's final assistant message.
//
// It satisfies the tools.SubagentSpawner interface.
//
// Note: SubagentSpawn / SubagentDone events are emitted on the PARENT writer
// (a.Writer) rather than carrying a ToolUseID, because the Tool.Execute
// signature does not expose the tool_use block ID. Parent/child linkage is
// established via the child session's ParentID and the ChildID field.
func (a *Agent) Spawn(ctx context.Context, task, systemPrompt string) (string, bool, error) {
	// Defensive depth check: subagents may not spawn further subagents.
	if a.Session != nil && a.Session.Depth >= 1 {
		return "", true, fmt.Errorf("subagent (depth %d) cannot spawn another subagent", a.Session.Depth)
	}

	// Generate a unique child ID.
	childID := fmt.Sprintf("sub-%d", a.nextSubIndex())

	// Build the child session from the parent's parameters.
	var parent *session.Session
	if a.Session != nil {
		parent = a.Session
	} else {
		// Fallback: create a minimal parent placeholder (should not occur in normal use).
		parent = session.NewSession("", ".", "", "")
	}

	child := session.NewSession(childID, parent.CWD(), parent.Model, parent.Endpoint)
	child.Depth = 1
	child.ParentID = parent.ID

	// Seed the child's history with the task as the first user message.
	taskMsg := common.Message{
		Role: common.RoleUser,
		Content: []common.Block{
			{Kind: common.BlockText, Text: task},
		},
	}
	child.History = []common.Message{taskMsg}

	// Open a JSONL writer for the child session.
	path := session.SessionPath(a.Home, parent.CWD(), childID, time.Now().UTC())
	cw, err := session.OpenWriter(path)
	if err != nil {
		return "", true, fmt.Errorf("spawn: open child writer: %w", err)
	}

	// Write the child session start event.
	cw.Emit(session.SessionStart{
		ID:        childID,
		ParentID:  parent.ID,
		CWD:       parent.CWD(),
		Model:     parent.Model,
		Endpoint:  parent.Endpoint,
		Depth:     1,
		StartedAt: child.StartedAt,
	})

	// Write the task as the child's first user message event.
	cw.Emit(session.UserMessage{Content: taskMsg.Content})

	// Resolve the system prompt.
	sys := systemPrompt
	if sys == "" {
		sys = a.System
	}

	// Build the child Agent (shares Client / Guard / Frontend with parent).
	childAgent := &Agent{
		Client:         a.Client,
		Tools:          a.Tools.Without("subagent"),
		Guard:          a.Guard,
		Frontend:       a.Frontend,
		Writer:         cw,
		System:         sys,
		MaxTokens:      a.MaxTokens,
		ThinkingBudget: a.ThinkingBudget,
		Home:           a.Home,
	}

	// Notify the frontend that a new child session has started.
	a.Frontend.Emit(childID, Event{
		Kind:      EventSessionStarted,
		SessionID: childID,
	})

	// Persist spawn marker on the PARENT writer (ToolUseID left empty for v1;
	// linkage is via ChildID + child.ParentID).
	if a.Writer != nil {
		a.Writer.Emit(session.SubagentSpawn{
			ChildID: childID,
			Task:    task,
		})
	}

	// Run the child agent.
	runErr := childAgent.Run(ctx, child)

	// Determine whether the run ended in error.
	isError := runErr != nil

	// Emit session_end on the child writer — exactly one per writer owner.
	endReason := "done"
	if errors.Is(runErr, context.Canceled) || ctx.Err() != nil {
		endReason = "cancelled"
	}
	cw.Emit(session.SessionEnd{Reason: endReason})

	// Persist done marker on the PARENT writer.
	if a.Writer != nil {
		a.Writer.Emit(session.SubagentDone{
			ChildID: childID,
			IsError: isError,
		})
	}

	// Always close the child writer regardless of run outcome.
	_ = cw.Close()

	if runErr != nil {
		return "", true, runErr
	}

	// Extract the text of the last assistant message.
	resultText := lastAssistantText(child)
	return resultText, false, nil
}

// lastAssistantText concatenates all BlockText blocks from the last assistant
// message in sess.History. Returns an empty string if there is none.
func lastAssistantText(sess *session.Session) string {
	for i := len(sess.History) - 1; i >= 0; i-- {
		msg := sess.History[i]
		if msg.Role != common.RoleAssistant {
			continue
		}
		var text string
		for _, b := range msg.Content {
			if b.Kind == common.BlockText {
				text += b.Text
			}
		}
		return text
	}
	return ""
}
