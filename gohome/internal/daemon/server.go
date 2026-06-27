// Package daemon provides the GoHome daemon server, which listens on a Unix
// socket and dispatches JSON-RPC requests from a connected TUI client.
package daemon

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/frontend"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/llm"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
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
	}

	if cfg.LLMClient != nil {
		if err := srv.initAgent(cfg); err != nil {
			ln.Close()
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
			log.Printf("daemon: accept error: %v", err)
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
		c.Close()
	}

	s.listener.Close()
}

// handleClient creates an rpc.Conn and frontend.RPCFrontend for the given
// connection, reads messages in a loop, and dispatches each one.
func (s *Server) handleClient(conn net.Conn) {
	s.cancelGraceTimer()

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
		// Send the RPC response before delivering input so the TUI's
		// sendInputCmd unblocks immediately, even if the agent is busy.
		_ = c.WriteResponse(rpc.Response{
			ID:     msg.ID,
			Result: json.RawMessage(`{}`),
		})
		s.mu.Lock()
		fe := s.fe
		s.mu.Unlock()
		if fe != nil {
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

	case rpc.MethodSessionApproval:
		// Approval responses flow through the JSON-RPC response path
		// (msg.IsResponse -> fe.ResolvePending), not as a separate request.
		// This method is unused in the current architecture.
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

// runLoop is the daemon's agent REPL: it waits for user input from the
// connected frontend, appends it to the session, and runs the agent. It
// returns when the server context is cancelled.
func (s *Server) runLoop() {
	for {
		sessID := s.agent.State.Session().ID

		s.mu.Lock()
		fe := s.fe
		s.mu.Unlock()

		if fe == nil {
			// No client connected yet; wait briefly and retry.
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
				continue
			}
		}

		// Set frontend on agent for this iteration.
		s.agent.Frontend = fe

		text, err := fe.AwaitUserInput(s.ctx, sessID)
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

// ---------- RPC handler methods ----------

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

	id := newSessionID()
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

// newSessionID generates an 8-char lowercase base32 session ID using crypto/rand.
func newSessionID() string {
	buf := make([]byte, 5) // 5 bytes -> 8 base32 chars (no padding)
	if _, err := rand.Read(buf); err != nil {
		panic("newSessionID: crypto/rand failed: " + err.Error())
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	return strings.ToLower(enc.EncodeToString(buf))
}

// cleanup removes the socket file.
func (s *Server) cleanup() {
	os.Remove(s.sockPath)
}
