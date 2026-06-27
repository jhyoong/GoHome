package daemon

import (
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

// dispatch routes an incoming JSON-RPC message to the appropriate handler.
func (s *Server) dispatch(c *rpc.Conn, msg *rpc.Message) {
	// If this is a response (no method), forward to the frontend's pending tracker.
	if msg.IsResponse() {
		if fe := s.frontend(); fe != nil {
			fe.ResolvePending(msg.ID.Int64(), msg.Result, msg.Error)
		}
		return
	}

	switch msg.Method {
	case rpc.MethodDaemonHealth:
		uptime := int64(time.Since(s.startedAt).Seconds())
		respondOK(c, msg.ID, rpc.HealthResult{
			Version:       s.config.Version,
			UptimeSeconds: uptime,
		})

	case rpc.MethodDaemonStop:
		respondOK(c, msg.ID, struct{}{})
		s.Stop()

	case rpc.MethodSessionInput:
		var params rpc.SessionInputParams
		if !unmarshalParams(c, msg.ID, msg.Params, &params) {
			return
		}
		// Send the RPC response before delivering input so the TUI's
		// sendInputCmd unblocks immediately, even if the agent is busy.
		respondOK(c, msg.ID, struct{}{})
		if fe := s.frontend(); fe != nil {
			fe.DeliverInput(params.Text)
		}

	case rpc.MethodSessionList:
		s.handleSessionList(c, msg)

	case rpc.MethodSessionNew:
		s.handleSessionNew(c, msg)

	case rpc.MethodSessionResume:
		s.handleSessionResume(c, msg)

	case rpc.MethodSessionCancel:
		s.handleSessionCancel(c, msg)

	case rpc.MethodModelSet:
		s.handleModelSet(c, msg)

	default:
		respondError(c, msg.ID, rpc.ErrMethodNotFound, "method not found")
	}
}

// initAgent builds the agent and its session state from the provided config.
func (s *Server) initAgent(cfg ServerConfig) error {
	sess := session.NewSession(cfg.SessionID, cfg.CWD, cfg.ModelName, cfg.ModelConfig)
	writerPath := session.SessionPath(cfg.Home, cfg.CWD, sess.ID, time.Now().UTC())
	writer, err := session.OpenWriter(writerPath)
	if err != nil {
		return err
	}
	writer.Emit(session.SessionStart{
		ID:        sess.ID,
		CWD:       cfg.CWD,
		StartedAt: sess.StartedAt,
	})

	state := agent.NewSessionState(sess, writer, cfg.LLMClient)

	s.agent = &agent.Agent{
		Tools:          cfg.Registry,
		Guard:          cfg.Guard,
		State:          state,
		System:         cfg.SystemPrompt,
		MaxTokens:      cfg.MaxTokens,
		ThinkingBudget: cfg.ThinkingBudget,
		Home:           cfg.Home,
	}
	s.agent.RegisterSubagentTool()
	return nil
}
