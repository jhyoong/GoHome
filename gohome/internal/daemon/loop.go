package daemon

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

// runLoop is the daemon's agent REPL: it waits for user input from the
// connected frontend, appends it to the session, and runs the agent. It
// returns when the server context is cancelled.
func (s *Server) runLoop() {
	for {
		fe := s.frontend()

		if fe == nil {
			select {
			case <-s.ctx.Done():
				return
			case <-s.feReady:
				continue
			}
		}

		// Set frontend on agent for this iteration.
		s.agent.SetFrontend(fe)

		text, err := fe.AwaitUserInput(s.ctx)
		if err != nil {
			if s.ctx.Err() != nil {
				return
			}
			continue
		}

		// Re-fetch after blocking: the session and writer may have been
		// swapped while we waited for input.
		sess := s.agent.State.Session()
		writer := s.agent.State.Writer()

		sess.History = append(sess.History, common.Message{
			Role: common.RoleUser,
			Content: []common.Block{
				{Kind: common.BlockText, Text: text},
			},
		})
		writer.Emit(session.UserMessage{
			Content: []common.Block{
				{Kind: common.BlockText, Text: text},
			},
		})

		turnCtx, cancel := context.WithCancel(s.ctx)
		s.turnMu.Lock()
		s.turnCancel = cancel
		s.turnMu.Unlock()

		runErr := s.agent.Run(turnCtx, sess)

		s.turnMu.Lock()
		s.turnCancel = nil
		s.turnMu.Unlock()
		cancel()

		if runErr != nil {
			slog.Error("daemon: agent run failed", "err", runErr)
			if s.ctx.Err() != nil {
				return
			}
		}

		if tag, drainErr := s.agent.State.DrainPending(); tag != "" {
			if drainErr != nil {
				slog.Error("daemon: session swap failed", "tag", tag, "err", drainErr)
			} else {
				newSess := s.agent.State.Session()
				fe.Emit(newSess.ID, agent.Event{
					Kind:      agent.EventSessionSwapped,
					SessionID: newSess.ID,
				})
			}
		}
	}
}

// sendStateSync sends a session.state notification to the given connection so
// a newly connected TUI client can display the correct model, yolo status,
// and session ID.
func (s *Server) sendStateSync(c *rpc.Conn) {
	if s.agent == nil {
		return
	}
	sess := s.agent.State.Session()
	model := s.agent.State.Model()

	yolo := false
	if s.agent.Guard != nil {
		yolo = s.agent.Guard.Yolo()
	}

	params := rpc.SessionStateParams{
		SessionID: sess.ID,
		Model:     model,
		Yolo:      yolo,
	}
	data, _ := json.Marshal(params)
	_ = c.Notify(rpc.MethodSessionState, data)
}
