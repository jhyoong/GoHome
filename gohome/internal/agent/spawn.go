package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// Spawn creates an isolated child agent for task, runs it to completion, and
// returns the text of the child's final assistant message.
//
// It satisfies the tools.SubagentSpawner interface.
//
// Note: SubagentSpawn / SubagentDone events are emitted on the parent session's
// writer (a.State.Writer()). Parent/child linkage is established via the child
// session's ParentID and ParentToolUseID (read from ctx, see
// tools.ToolUseIDFrom) and the ChildID field.
func (a *Agent) Spawn(ctx context.Context, task, systemPrompt string) (string, bool, error) {
	// Fetch the parent session from State.
	parent := a.State.Session()

	// Defensive depth check: subagents may not spawn further subagents.
	if parent != nil && parent.Depth >= 1 {
		return "", true, fmt.Errorf("subagent (depth %d) cannot spawn another subagent", parent.Depth)
	}

	// Fallback: create a minimal parent placeholder (should not occur in normal use).
	if parent == nil {
		parent = session.NewSession("", ".", "", "")
	}

	// Generate a unique child ID: "<parentID>-sub-N". Parent IDs are random,
	// so child file names do not collide across runs. Child files already on
	// disk also count: a crash can leave one without a spawn marker.
	if existing, err := session.ListChildren(a.Home, parent.CWD(), parent.ID); err == nil {
		for _, l := range existing {
			parent.NoteSubagentID(l.ID)
		}
	}
	childID := parent.NextSubagentID()

	// The child uses the parent's active model settings.
	ms := a.State.ModelSettings()
	child := session.NewSession(childID, parent.CWD(), ms.Name, ms.ConfigName)
	child.Depth = 1
	child.ParentID = parent.ID
	child.ParentToolUseID = tools.ToolUseIDFrom(ctx)

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
		ID:              childID,
		ParentID:        parent.ID,
		ParentToolUseID: child.ParentToolUseID,
		CWD:             parent.CWD(),
		Model:           ms.Name,
		ModelConfig:     ms.ConfigName,
		Depth:           1,
		StartedAt:       child.StartedAt,
	})

	// Write the task as the child's first user message event.
	cw.Emit(session.UserMessage{Content: taskMsg.Content})

	// Resolve the system prompt.
	sys := systemPrompt
	if sys == "" {
		sys = a.System
	}

	// Build the child Agent (shares Guard / Frontend with parent). The child
	// gets no steer channel: steers are for the main run, which picks them up
	// after the subagent returns.
	childState := NewSessionState(child, cw, a.State.Client())
	childState.SetModelSettings(ms)
	childAgent := &Agent{
		Tools:         a.Tools.Without("subagent"),
		Guard:         a.Guard,
		Frontend:      noSteerFrontend{a.Frontend},
		State:         childState,
		System:        sys,
		CompactCfg:    a.CompactCfg,
		CompactPrompt: a.CompactPrompt,
		Home:          a.Home,
	}

	// Notify the frontend that a new child session has started.
	a.Frontend.Emit(childID, Event{
		Kind:      EventSessionStarted,
		SessionID: childID,
	})

	// Persist spawn marker on the PARENT writer (ToolUseID left empty for v1;
	// linkage is via ChildID + child.ParentID).
	if w := a.State.Writer(); w != nil {
		w.Emit(session.SubagentSpawn{
			ChildID: childID,
			Task:    task,
		})
	}

	// Run the child agent.
	runErr := childAgent.Run(ctx, child)

	// Determine end reason before emitting.
	endReason := "done"
	if errors.Is(runErr, context.Canceled) || ctx.Err() != nil {
		endReason = "cancelled"
	}

	a.Frontend.Emit(childID, Event{
		Kind:      EventSessionEnded,
		SessionID: childID,
		EndReason: endReason,
	})

	// Determine whether the run ended in error.
	isError := runErr != nil

	// Emit session_end on the child writer.
	cw.Emit(session.SessionEnd{Reason: endReason})

	// Persist done marker on the PARENT writer.
	if w := a.State.Writer(); w != nil {
		w.Emit(session.SubagentDone{
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

// noSteerFrontend wraps a Frontend so SteerCh returns nil. Child agents use
// it so they never read steers meant for the parent.
type noSteerFrontend struct {
	Frontend
}

// SteerCh returns nil: a child agent has no steer channel.
func (noSteerFrontend) SteerCh() <-chan string { return nil }

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
