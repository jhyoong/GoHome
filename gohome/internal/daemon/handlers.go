package daemon

import (
	"encoding/json"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
	"github.com/jhyoong/GoHome/gohome/internal/llm"
	"github.com/jhyoong/GoHome/gohome/internal/session"
)

// handleSessionList returns the list of available sessions for the current cwd.
func (s *Server) handleSessionList(c *rpc.Conn, msg *rpc.Message) {
	listings, err := session.List(s.config.Home, s.config.CWD)
	if err != nil {
		_ = c.WriteResponse(rpc.Response{
			ID: msg.ID,
			Error: &rpc.Error{
				Code:    -32000,
				Message: "session list failed: " + err.Error(),
			},
		})
		return
	}
	result, _ := json.Marshal(rpc.SessionListResult{Sessions: listings})
	_ = c.WriteResponse(rpc.Response{ID: msg.ID, Result: result})
}

// handleSessionNew creates a new session, swaps it into the agent state, and
// returns the new session ID.
func (s *Server) handleSessionNew(c *rpc.Conn, msg *rpc.Message) {
	if s.agent == nil {
		_ = c.WriteResponse(rpc.Response{
			ID:    msg.ID,
			Error: &rpc.Error{Code: -32000, Message: "no agent configured"},
		})
		return
	}

	id := session.NewID()
	currentModel := s.agent.State.Model()
	currentCfg := s.agent.State.ModelConfig()
	newSess := session.NewSession(id, s.config.CWD, currentModel, currentCfg)
	wrPath := session.SessionPath(s.config.Home, s.config.CWD, id, time.Now().UTC())

	_, err := s.agent.State.Swap("new "+id, func(oldSess *session.Session, oldWriter *session.Writer) (*session.Session, *session.Writer, error) {
		oldWriter.Emit(session.SessionEnd{Reason: "switch"})
		_ = oldWriter.Close()
		newWriter, err := session.OpenWriter(wrPath)
		if err != nil {
			return nil, nil, err
		}
		newWriter.Emit(session.SessionStart{
			ID:          newSess.ID,
			CWD:         s.config.CWD,
			Model:       currentModel,
			ModelConfig: currentCfg,
			StartedAt:   newSess.StartedAt,
		})
		return newSess, newWriter, nil
	})
	if err != nil {
		_ = c.WriteResponse(rpc.Response{
			ID:    msg.ID,
			Error: &rpc.Error{Code: -32000, Message: "session new failed: " + err.Error()},
		})
		return
	}

	result, _ := json.Marshal(rpc.SessionNewResult{SessionID: id})
	_ = c.WriteResponse(rpc.Response{ID: msg.ID, Result: result})
}

// handleSessionResume loads a session from its JSONL file, swaps it into the
// agent state, and returns the session ID and history.
func (s *Server) handleSessionResume(c *rpc.Conn, msg *rpc.Message) {
	if s.agent == nil {
		_ = c.WriteResponse(rpc.Response{
			ID:    msg.ID,
			Error: &rpc.Error{Code: -32000, Message: "no agent configured"},
		})
		return
	}

	var params rpc.SessionResumeParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		_ = c.WriteResponse(rpc.Response{
			ID:    msg.ID,
			Error: &rpc.Error{Code: -32602, Message: "invalid params: " + err.Error()},
		})
		return
	}

	listings, err := session.List(s.config.Home, s.config.CWD)
	if err != nil {
		_ = c.WriteResponse(rpc.Response{
			ID:    msg.ID,
			Error: &rpc.Error{Code: -32000, Message: "session list failed: " + err.Error()},
		})
		return
	}

	var path string
	for _, l := range listings {
		if l.ID == params.ID {
			path = l.Path
			break
		}
	}
	if path == "" {
		_ = c.WriteResponse(rpc.Response{
			ID:    msg.ID,
			Error: &rpc.Error{Code: -32000, Message: "session not found: " + params.ID},
		})
		return
	}

	loaded, history, err := session.Load(path)
	if err != nil {
		_ = c.WriteResponse(rpc.Response{
			ID:    msg.ID,
			Error: &rpc.Error{Code: -32000, Message: "session load failed: " + err.Error()},
		})
		return
	}

	_, err = s.agent.State.Swap("resume "+loaded.ID, func(oldSess *session.Session, oldWriter *session.Writer) (*session.Session, *session.Writer, error) {
		oldWriter.Emit(session.SessionEnd{Reason: "switch"})
		_ = oldWriter.Close()
		newWriter, err := session.OpenWriter(path)
		if err != nil {
			return nil, nil, err
		}
		return loaded, newWriter, nil
	})
	if err != nil {
		_ = c.WriteResponse(rpc.Response{
			ID:    msg.ID,
			Error: &rpc.Error{Code: -32000, Message: "session resume failed: " + err.Error()},
		})
		return
	}

	historyJSON, _ := json.Marshal(history)
	result, _ := json.Marshal(rpc.SessionResumeResult{SessionID: loaded.ID, History: historyJSON})
	_ = c.WriteResponse(rpc.Response{ID: msg.ID, Result: result})
}

// handleSessionCancel cancels the current agent turn and clears any pending
// session swap.
func (s *Server) handleSessionCancel(c *rpc.Conn, msg *rpc.Message) {
	s.turnMu.Lock()
	if s.turnCancel != nil {
		s.turnCancel()
		s.turnCancel = nil
	}
	s.turnMu.Unlock()

	if s.agent != nil {
		s.agent.State.ClearPending()
	}

	_ = c.WriteResponse(rpc.Response{ID: msg.ID, Result: json.RawMessage(`{}`)})
}

// handleModelSet looks up a model config by name, creates a new LLM client,
// and updates the agent state.
func (s *Server) handleModelSet(c *rpc.Conn, msg *rpc.Message) {
	if s.agent == nil {
		_ = c.WriteResponse(rpc.Response{
			ID:    msg.ID,
			Error: &rpc.Error{Code: -32000, Message: "no agent configured"},
		})
		return
	}

	var params rpc.ModelSetParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		_ = c.WriteResponse(rpc.Response{
			ID:    msg.ID,
			Error: &rpc.Error{Code: -32602, Message: "invalid params: " + err.Error()},
		})
		return
	}

	cfg, ok := s.config.Settings.ModelConfig[params.Name]
	if !ok {
		_ = c.WriteResponse(rpc.Response{
			ID:    msg.ID,
			Error: &rpc.Error{Code: -32000, Message: "model config not found: " + params.Name},
		})
		return
	}

	apiKey, err := config.ResolveAPIKey(cfg)
	if err != nil {
		_ = c.WriteResponse(rpc.Response{
			ID:    msg.ID,
			Error: &rpc.Error{Code: -32000, Message: "no API key for model config: " + params.Name},
		})
		return
	}

	newClient, err := llm.New(cfg, apiKey)
	if err != nil {
		_ = c.WriteResponse(rpc.Response{
			ID:    msg.ID,
			Error: &rpc.Error{Code: -32000, Message: "cannot create LLM client: " + err.Error()},
		})
		return
	}

	s.agent.State.SetClient(newClient)
	s.agent.State.SetModel(cfg.ModelName)
	s.agent.State.SetModelConfig(params.Name)

	ctxWin := cfg.ContextWindow
	if ctxWin <= 0 {
		ctxWin = config.DefaultContextWindow
	}

	result, _ := json.Marshal(rpc.ModelSetResult{ModelName: cfg.ModelName, ContextWindow: ctxWin})
	_ = c.WriteResponse(rpc.Response{ID: msg.ID, Result: result})
}
