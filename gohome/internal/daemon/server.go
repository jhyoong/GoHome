// Package daemon provides the GoHome daemon server, which listens on a Unix
// socket and dispatches JSON-RPC requests from a connected TUI client.
package daemon

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/frontend"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
)

// ServerConfig holds configuration for the daemon server.
type ServerConfig struct {
	Version     string
	GracePeriod time.Duration // default 30s if zero

	// Agent dependencies (pre-built by caller). When LLMClient is non-nil the
	// server builds an agent on startup and runs the agent loop when the first
	// client connects. When LLMClient is nil the server runs without an agent
	// (useful for tests and headless health-check mode).
	LLMClient      common.Client
	Guard          *guard.Guard
	Registry       *tools.Registry
	SystemPrompt   string
	MaxTokens      int
	ThinkingBudget int
	Home           string
	CWD            string
	SessionID      string

	// Settings, ModelConfig, and ModelName are needed for model.set to look up
	// model configs, resolve API keys, and create new LLM clients.
	Settings    config.Settings
	ModelConfig string // current model config name
	ModelName   string // LLM model name (e.g. "claude-sonnet-4-5-20250514")
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

	graceTimer *time.Timer // started after client disconnect; fires auto-shutdown
	feReady    chan struct{}

	// Agent-related fields (populated only when ServerConfig.LLMClient is set).
	agent      *agent.Agent
	turnMu     sync.Mutex
	turnCancel context.CancelFunc
	agentOnce  sync.Once // ensures runLoop starts only once
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

	srv := &Server{
		listener:  ln,
		sockPath:  sockPath,
		config:    cfg,
		startedAt: time.Now(),
		cancel:    cancel,
		ctx:       ctx,
		feReady:   make(chan struct{}, 1),
	}

	if cfg.LLMClient != nil {
		if err := srv.initAgent(cfg); err != nil {
			_ = ln.Close()
			return nil, err
		}
	}

	return srv, nil
}

// Serve accepts connections in a loop until the server context is cancelled.
// It calls cleanup on exit to remove the socket file.
func (s *Server) Serve() {
	defer s.cleanup()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.ctx.Err() != nil {
				return
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			slog.Error("daemon: accept error", "error", err)
			continue
		}

		s.cancelGraceTimer()
		s.handleClient(conn)
		if s.ctx.Err() != nil {
			return
		}
		s.startGraceTimer()
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
		_ = c.Close()
	}

	_ = s.listener.Close()
}

// handleClient creates an rpc.Conn and frontend.RPCFrontend for the given
// connection, reads messages in a loop, and dispatches each one.
func (s *Server) handleClient(conn net.Conn) {
	c := rpc.NewConn(conn)
	fe := frontend.New(c)

	// Set the RPCFrontend as the guard's frontend for tool approval.
	if s.agent != nil && s.agent.Guard != nil {
		s.agent.Guard.SetFrontend(fe)
	}

	s.mu.Lock()
	s.client = c
	s.fe = fe
	s.mu.Unlock()

	select {
	case s.feReady <- struct{}{}:
	default:
	}

	// Send current state to the new client so it can display the correct
	// model name, yolo status, and session ID immediately.
	s.sendStateSync(c)

	// Start the agent loop on first connection (if agent is configured).
	if s.agent != nil {
		s.agentOnce.Do(func() {
			go s.runLoop()
		})
	}

	defer func() {
		s.mu.Lock()
		s.client = nil
		s.fe = nil
		s.mu.Unlock()
		fe.Close()
		_ = c.Close()
	}()

	for {
		msg, err := c.Read()
		if err != nil {
			return
		}
		s.dispatch(c, msg)
	}
}

// ---------- Grace period ----------

func (s *Server) startGraceTimer() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.graceTimer = time.AfterFunc(s.config.GracePeriod, func() {
		s.turnMu.Lock()
		turnActive := s.turnCancel != nil
		s.turnMu.Unlock()
		if !turnActive {
			slog.Info("daemon: grace period expired, shutting down")
			s.Stop()
		}
	})
}

func (s *Server) cancelGraceTimer() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.graceTimer != nil {
		s.graceTimer.Stop()
		s.graceTimer = nil
	}
}

// ---------- Helpers ----------

// cleanup removes the socket file.
func (s *Server) cleanup() {
	_ = os.Remove(s.sockPath)
}
