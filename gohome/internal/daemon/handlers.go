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
		respondError(c, msg.ID, rpc.ErrServerError, "session list failed: "+err.Error())
		return
	}
	respondOK(c, msg.ID, rpc.SessionListResult{Sessions: listings})
}

// handleSessionNew creates a new session, swaps it into the agent state, and
// returns the new session ID.
func (s *Server) handleSessionNew(c *rpc.Conn, msg *rpc.Message) {
	if !s.requireAgent(c, msg.ID) {
		return
	}

	id := session.NewID()
	currentModel := s.agent.State.Model()
	currentCfg := s.agent.State.ModelConfig()
	newSess := session.NewSession(id, s.config.CWD, currentModel, currentCfg)
	wrPath := session.SessionPath(s.config.Home, s.config.CWD, id, time.Now().UTC())

	err := s.agent.State.Swap("new "+id, func(oldSess *session.Session, oldWriter *session.Writer) (*session.Session, *session.Writer, error) {
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
		oldWriter.Emit(session.SessionEnd{Reason: "switch"})
		_ = oldWriter.Close()
		return newSess, newWriter, nil
	})
	if err != nil {
		respondError(c, msg.ID, rpc.ErrServerError, "session new failed: "+err.Error())
		return
	}

	respondOK(c, msg.ID, rpc.SessionNewResult{SessionID: id})
}

// handleSessionResume loads a session from its JSONL file, swaps it into the
// agent state, and returns the session ID and history.
func (s *Server) handleSessionResume(c *rpc.Conn, msg *rpc.Message) {
	if !s.requireAgent(c, msg.ID) {
		return
	}

	var params rpc.SessionResumeParams
	if !unmarshalParams(c, msg.ID, msg.Params, &params) {
		return
	}

	listings, err := session.List(s.config.Home, s.config.CWD)
	if err != nil {
		respondError(c, msg.ID, rpc.ErrServerError, "session list failed: "+err.Error())
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
		respondError(c, msg.ID, rpc.ErrServerError, "session not found: "+params.ID)
		return
	}

	loaded, history, err := session.Load(path)
	if err != nil {
		respondError(c, msg.ID, rpc.ErrServerError, "session load failed: "+err.Error())
		return
	}

	err = s.agent.State.Swap("resume "+loaded.ID, func(oldSess *session.Session, oldWriter *session.Writer) (*session.Session, *session.Writer, error) {
		newWriter, err := session.OpenWriter(path)
		if err != nil {
			return nil, nil, err
		}
		oldWriter.Emit(session.SessionEnd{Reason: "switch"})
		_ = oldWriter.Close()
		return loaded, newWriter, nil
	})
	if err != nil {
		respondError(c, msg.ID, rpc.ErrServerError, "session resume failed: "+err.Error())
		return
	}

	historyJSON, _ := json.Marshal(history)
	respondOK(c, msg.ID, rpc.SessionResumeResult{SessionID: loaded.ID, History: historyJSON})
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

	respondOK(c, msg.ID, struct{}{})
}

// handleYoloSet updates the guard's yolo mode.
func (s *Server) handleYoloSet(c *rpc.Conn, msg *rpc.Message) {
	var params rpc.YoloSetParams
	if !unmarshalParams(c, msg.ID, msg.Params, &params) {
		return
	}
	if s.agent != nil && s.agent.Guard != nil {
		s.agent.Guard.SetYolo(params.Enabled)
	}
	respondOK(c, msg.ID, struct{}{})
}

// handleModelSet looks up a model config by name, creates a new LLM client,
// and updates the agent state.
func (s *Server) handleModelSet(c *rpc.Conn, msg *rpc.Message) {
	if !s.requireAgent(c, msg.ID) {
		return
	}

	var params rpc.ModelSetParams
	if !unmarshalParams(c, msg.ID, msg.Params, &params) {
		return
	}

	cfg, ok := s.config.Settings.ModelConfig[params.Name]
	if !ok {
		respondError(c, msg.ID, rpc.ErrServerError, "model config not found: "+params.Name)
		return
	}

	apiKey, err := config.ResolveAPIKey(cfg)
	if err != nil {
		respondError(c, msg.ID, rpc.ErrServerError, "no API key for model config: "+params.Name)
		return
	}

	newClient, err := llm.New(cfg, apiKey)
	if err != nil {
		respondError(c, msg.ID, rpc.ErrServerError, "cannot create LLM client: "+err.Error())
		return
	}

	s.agent.State.SetClient(newClient)
	s.agent.State.SetModel(cfg.ModelName)
	s.agent.State.SetModelConfig(params.Name)

	ctxWin := cfg.ContextWindow
	if ctxWin <= 0 {
		ctxWin = config.DefaultContextWindow
	}

	respondOK(c, msg.ID, rpc.ModelSetResult{ModelName: cfg.ModelName, ContextWindow: ctxWin})
}
