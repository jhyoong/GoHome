package daemon

import (
	"encoding/json"

	"github.com/jhyoong/GoHome/gohome/internal/daemon/frontend"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
)

func respondError(c *rpc.Conn, id *rpc.ID, code int, msg string) {
	_ = c.WriteResponse(rpc.Response{
		ID:    id,
		Error: &rpc.Error{Code: code, Message: msg},
	})
}

func respondOK(c *rpc.Conn, id *rpc.ID, result any) {
	data, _ := json.Marshal(result)
	_ = c.WriteResponse(rpc.Response{ID: id, Result: data})
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
