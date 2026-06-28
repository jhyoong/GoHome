package daemon

import (
	"encoding/json"
	"log/slog"

	"github.com/jhyoong/GoHome/gohome/internal/daemon/frontend"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
)

func respondError(c *rpc.Conn, id *rpc.ID, code int, msg string) {
	if err := c.WriteResponse(rpc.Response{
		ID:    id,
		Error: &rpc.Error{Code: code, Message: msg},
	}); err != nil {
		slog.Error("respondError: write failed", "error", err)
	}
}

func respondOK(c *rpc.Conn, id *rpc.ID, result any) {
	data, err := json.Marshal(result)
	if err != nil {
		slog.Error("respondOK: marshal failed", "error", err)
		return
	}
	if err := c.WriteResponse(rpc.Response{ID: id, Result: data}); err != nil {
		slog.Error("respondOK: write failed", "error", err)
	}
}

func unmarshalParams(c *rpc.Conn, id *rpc.ID, raw json.RawMessage, v any) bool {
	if err := json.Unmarshal(raw, v); err != nil {
		respondError(c, id, rpc.ErrInvalidParams, "invalid params: "+err.Error())
		return false
	}
	return true
}

func (s *Server) requireAgent(c *rpc.Conn, id *rpc.ID) bool {
	if s.agent == nil {
		respondError(c, id, rpc.ErrServerError, "no agent configured")
		return false
	}
	return true
}

func (s *Server) frontend() *frontend.RPCFrontend {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fe
}
