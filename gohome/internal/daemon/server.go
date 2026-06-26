// Package daemon provides the GoHome daemon server, which listens on a Unix
// socket and dispatches JSON-RPC requests from a connected TUI client.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/daemon/frontend"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
)

// ServerConfig holds configuration for the daemon server.
type ServerConfig struct {
	Version     string
	GracePeriod time.Duration // default 30s if zero
}

// Server listens on a Unix socket, accepts a single client connection, and
// dispatches incoming JSON-RPC requests.
type Server struct {
	listener  net.Listener
	sockPath  string
	config    ServerConfig
	startedAt time.Time
	cancel    context.CancelFunc
	ctx       context.Context
	mu        sync.Mutex // guards client and fe
	client    *rpc.Conn
	fe        *frontend.RPCFrontend
}

// NewServer creates a daemon server that listens on the given Unix socket path.
// It removes any stale socket file at sockPath before binding.
func NewServer(sockPath string, cfg ServerConfig) (*Server, error) {
	if cfg.GracePeriod == 0 {
		cfg.GracePeriod = 30 * time.Second
	}

	// Remove stale socket file if it exists.
	if err := os.Remove(sockPath); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Server{
		listener:  ln,
		sockPath:  sockPath,
		config:    cfg,
		startedAt: time.Now(),
		cancel:    cancel,
		ctx:       ctx,
	}, nil
}

// Serve accepts connections in a loop until the server context is cancelled.
// It calls cleanup on exit to remove the socket file.
func (s *Server) Serve() {
	defer s.cleanup()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			// Check if we were stopped intentionally.
			if s.ctx.Err() != nil {
				return
			}
			// Listener closed for another reason (e.g. Stop called).
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("daemon: accept error: %v", err)
			continue
		}

		s.handleClient(conn)
	}
}

// Stop cancels the server context, closes any active client connection, and
// closes the listener, causing Serve to return.
func (s *Server) Stop() {
	s.cancel()

	// Close the active client connection so handleClient's Read unblocks.
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	if c != nil {
		c.Close()
	}

	s.listener.Close()
}

// handleClient creates an rpc.Conn and frontend.RPCFrontend for the given
// connection, reads messages in a loop, and dispatches each one.
func (s *Server) handleClient(conn net.Conn) {
	c := rpc.NewConn(conn)
	fe := frontend.New(c)

	s.mu.Lock()
	s.client = c
	s.fe = fe
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.client = nil
		s.fe = nil
		s.mu.Unlock()
		c.Close()
	}()

	for {
		msg, err := c.Read()
		if err != nil {
			// Client disconnected or read error.
			if s.ctx.Err() != nil {
				return
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			return
		}
		s.dispatch(c, msg)
	}
}

// dispatch routes an incoming JSON-RPC message to the appropriate handler.
func (s *Server) dispatch(c *rpc.Conn, msg *rpc.Message) {
	// If this is a response (no method), forward to the frontend's pending tracker.
	if msg.IsResponse() {
		s.mu.Lock()
		fe := s.fe
		s.mu.Unlock()
		if fe != nil {
			fe.ResolvePending(msg.ID.Int64(), msg.Result, msg.Error)
		}
		return
	}

	switch msg.Method {
	case rpc.MethodDaemonHealth:
		uptime := int64(time.Since(s.startedAt).Seconds())
		result := rpc.HealthResult{
			Version:       s.config.Version,
			UptimeSeconds: uptime,
		}
		data, _ := json.Marshal(result)
		_ = c.WriteResponse(rpc.Response{
			ID:     msg.ID,
			Result: data,
		})

	case rpc.MethodDaemonStop:
		_ = c.WriteResponse(rpc.Response{
			ID:     msg.ID,
			Result: json.RawMessage(`{}`),
		})
		s.Stop()

	case rpc.MethodSessionInput:
		var params rpc.SessionInputParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			_ = c.WriteResponse(rpc.Response{
				ID: msg.ID,
				Error: &rpc.Error{
					Code:    -32602,
					Message: "invalid params: " + err.Error(),
				},
			})
			return
		}
		s.mu.Lock()
		fe := s.fe
		s.mu.Unlock()
		if fe != nil {
			fe.DeliverInput(params.Text)
		}
		_ = c.WriteResponse(rpc.Response{
			ID:     msg.ID,
			Result: json.RawMessage(`{}`),
		})

	case rpc.MethodSessionNew,
		rpc.MethodSessionResume,
		rpc.MethodSessionList,
		rpc.MethodSessionCancel,
		rpc.MethodSessionApproval,
		rpc.MethodModelSet:
		// Will be implemented in Task 13.
		_ = c.WriteResponse(rpc.Response{
			ID:     msg.ID,
			Result: json.RawMessage(`{}`),
		})

	default:
		_ = c.WriteResponse(rpc.Response{
			ID: msg.ID,
			Error: &rpc.Error{
				Code:    -32601,
				Message: "method not found",
			},
		})
	}
}

// cleanup removes the socket file.
func (s *Server) cleanup() {
	os.Remove(s.sockPath)
}
