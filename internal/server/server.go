package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jhyoong/gohome/internal/agent"
	"github.com/jhyoong/gohome/internal/config"
	"github.com/jhyoong/gohome/internal/llm"
	"github.com/jhyoong/gohome/internal/session"
	"github.com/jhyoong/gohome/internal/tools"
)

const (
	pingInterval = 30 * time.Second
	pongWait     = 40 * time.Second
	writeWait    = 10 * time.Second
)

type Config struct {
	Store                *session.Store
	LLMClient            *llm.Client
	Registry             *tools.Registry
	SystemPrompt         string
	SubagentSystemPrompt string
	Approval             config.ApprovalConfig
	FullConfig           *config.Config // pointer to full config for persisting whitelist; set to nil to disable disk writes
	ConfigPath           string         // original path for saving, e.g. "~/.gohome/config.yaml"
	ContextWindow        int            // max context window in tokens
}

type Server struct {
	cfg            Config
	approvalMu     sync.RWMutex // protects cfg.Approval.Whitelist across connections
	sessionHubs    sync.Map     // sessionID → *SessionHub
	globalWatchers sync.Map     // tabID → *wsConn
}

func New(cfg Config) *Server {
	return &Server{cfg: cfg}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)
	mux.HandleFunc("/ws", s.handleWebSocket)
	return mux
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.cfg.Store.ListSessions(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sessions == nil {
		sessions = []session.Session{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	sess, err := s.cfg.Store.CreateSession(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(sess)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.cfg.Store.DeleteSession(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// persistWhitelistEntry appends to the server's shared whitelist and saves to disk.
func (s *Server) persistWhitelistEntry(entry config.WhitelistEntry) {
	s.approvalMu.Lock()
	s.cfg.Approval.Whitelist = append(s.cfg.Approval.Whitelist, entry)
	if s.cfg.FullConfig != nil {
		s.cfg.FullConfig.Approval.Whitelist = s.cfg.Approval.Whitelist
		if err := config.Save(s.cfg.ConfigPath, s.cfg.FullConfig); err != nil {
			log.Printf("always_allow: failed to save config: %v", err)
		}
	}
	s.approvalMu.Unlock()

	// Propagate to every live hub so existing brokers honor the new entry
	// for in-flight requests. Newly-created hubs pick it up from cfg.
	s.sessionHubs.Range(func(_, v any) bool {
		v.(*SessionHub).Broker().AddWhitelistEntry(entry)
		return true
	})
}

// getOrCreateHub returns the SessionHub for sessionID, creating one on
// first request. The first creator's hub is the one returned; concurrent
// callers see the same instance (sync.Map LoadOrStore). The newly-created
// hub's dispatch loop is started exactly once.
func (s *Server) getOrCreateHub(sessionID string) *SessionHub {
	if existing, ok := s.sessionHubs.Load(sessionID); ok {
		return existing.(*SessionHub)
	}
	s.approvalMu.RLock()
	cfg := s.cfg.Approval
	s.approvalMu.RUnlock()
	candidate := NewSessionHub(sessionID, cfg)
	candidate.GlobalWatchers = s.snapshotGlobalWatchers
	actual, loaded := s.sessionHubs.LoadOrStore(sessionID, candidate)
	hub := actual.(*SessionHub)
	if !loaded {
		go hub.Run()
	}
	return hub
}

// removeHubIfIdle deletes the hub for sessionID if and only if it has no
// active agent runs, no watchers, and no pending approvals. Uses
// CompareAndDelete to avoid removing a hub that a concurrent goroutine
// has just stored.
func (s *Server) removeHubIfIdle(sessionID string) {
	val, ok := s.sessionHubs.Load(sessionID)
	if !ok {
		return
	}
	hub := val.(*SessionHub)
	if !hub.Idle() {
		return
	}
	if s.sessionHubs.CompareAndDelete(sessionID, hub) {
		hub.Stop()
	}
}

// snapshotGlobalWatchers returns a slice of all currently-connected
// wsConn. Used by hubs to address toast notifications to tabs that are
// NOT watching this hub's session.
func (s *Server) snapshotGlobalWatchers() []*wsConn {
	var out []*wsConn
	s.globalWatchers.Range(func(_, v any) bool {
		out = append(out, v.(*wsConn))
		return true
	})
	return out
}

type inMsg struct {
	Type           string `json:"type"`
	SessionID      string `json:"session_id,omitempty"`
	Content        string `json:"content,omitempty"`
	RequestID      string `json:"request_id,omitempty"`
	Approved       bool   `json:"approved,omitempty"`
	Tool           string `json:"tool,omitempty"`
	CommandPattern string `json:"command_pattern,omitempty"`
}

type outMsg struct {
	Type             string          `json:"type"`
	Data             any             `json:"data,omitempty"`
	RequestID        string          `json:"request_id,omitempty"`
	Tool             string          `json:"tool,omitempty"`
	Params           json.RawMessage `json:"params,omitempty"`
	Result           string          `json:"result,omitempty"`
	Approved         bool            `json:"approved,omitempty"`
	Message          string          `json:"message,omitempty"`
	MessageID        string          `json:"message_id,omitempty"`
	SessionID        string          `json:"session_id,omitempty"`
	Messages         any             `json:"messages,omitempty"`
	PromptTokens     int             `json:"prompt_tokens,omitempty"`
	CompletionTokens int             `json:"completion_tokens,omitempty"`
	ContextWindow    int             `json:"context_window,omitempty"`
	ping             bool
}

type wsConn struct {
	conn     *websocket.Conn
	tabID    string
	inbound  chan inMsg
	outbound chan outMsg
	store    *session.Store
	server   *Server

	mu        sync.Mutex
	running   bool
	runCancel context.CancelFunc
	steerCh   chan string

	// watching is the set of hubs this connection is currently subscribed
	// to. In practice a tab views one session at a time, but the map keeps
	// switch-session transitions safe and idempotent.
	watching map[string]*SessionHub // keyed by sessionID
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	tabID := r.URL.Query().Get("tab")
	if tabID == "" {
		http.Error(w, "missing tab query parameter", http.StatusBadRequest)
		return
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			// Allow localhost and local network connections
			return strings.HasPrefix(origin, "http://localhost") ||
				strings.HasPrefix(origin, "http://127.0.0.1") ||
				strings.HasPrefix(origin, "https://localhost") ||
				strings.HasPrefix(origin, "https://127.0.0.1") ||
				strings.HasPrefix(origin, "http://192.168.")
		},
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WS upgrade: %v", err)
		return
	}

	ws := &wsConn{
		conn:     conn,
		tabID:    tabID,
		inbound:  make(chan inMsg, 16),
		outbound: make(chan outMsg, 256),
		store:    s.cfg.Store,
		server:   s,
		watching: make(map[string]*SessionHub),
	}

	s.globalWatchers.Store(tabID, ws)
	defer s.globalWatchers.Delete(tabID)
	defer ws.unwatchAll()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go ws.reader(ctx, cancel)
	go ws.writer(ctx)
	go ws.pingLoop(ctx)
	ws.dispatcher(ctx)
}

func (wc *wsConn) reader(ctx context.Context, cancel context.CancelFunc) {
	defer cancel()
	wc.conn.SetReadDeadline(time.Now().Add(pongWait))
	wc.conn.SetPongHandler(func(string) error {
		wc.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		var msg inMsg
		if err := wc.conn.ReadJSON(&msg); err != nil {
			return
		}
		select {
		case wc.inbound <- msg:
		case <-ctx.Done():
			return
		}
	}
}

func (wc *wsConn) writer(ctx context.Context) {
	for {
		select {
		case msg := <-wc.outbound:
			wc.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if msg.ping {
				if err := wc.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			} else {
				if err := wc.conn.WriteJSON(msg); err != nil {
					return
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

func (wc *wsConn) pingLoop(ctx context.Context) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			select {
			case wc.outbound <- outMsg{ping: true}:
			default:
			}
		case <-ctx.Done():
			return
		}
	}
}

func (wc *wsConn) dispatcher(ctx context.Context) {
	for {
		select {
		case msg := <-wc.inbound:
			switch msg.Type {
			case "message":
				wc.mu.Lock()
				if wc.running {
					ch := wc.steerCh
					wc.mu.Unlock()
					if ch != nil {
						select {
						case ch <- msg.Content:
						default:
						}
					}
					continue
				}
				steerCh := make(chan string, 8)
				runCtx, cancel := context.WithCancel(ctx)
				wc.running = true
				wc.steerCh = steerCh
				wc.runCancel = cancel
				wc.mu.Unlock()

				sessionID := msg.SessionID
				isNew := false
				if sessionID == "" {
					sess, err := wc.store.CreateSession(ctx)
					if err != nil {
						wc.send(outMsg{Type: "error", Message: err.Error()})
						cancel()
						wc.mu.Lock()
						wc.running = false
						wc.runCancel = nil
						wc.steerCh = nil
						wc.mu.Unlock()
						continue
					}
					sessionID = sess.ID
					isNew = true
					sessions, _ := wc.store.ListSessions(ctx)
					if sessions == nil {
						sessions = []session.Session{}
					}
					wc.send(outMsg{Type: "session_created", SessionID: sessionID})
					wc.send(outMsg{Type: "sessions", Data: sessions})
				}
				wc.watchSession(sessionID)
				go wc.runAgent(runCtx, sessionID, msg.Content, steerCh, isNew)

			case "stop":
				wc.mu.Lock()
				if wc.runCancel != nil {
					wc.runCancel()
				}
				wc.mu.Unlock()

			case "tool_response":
				wc.mu.Lock()
				hubs := make([]*SessionHub, 0, len(wc.watching))
				for _, h := range wc.watching {
					hubs = append(hubs, h)
				}
				wc.mu.Unlock()
				for _, h := range hubs {
					if h.Respond(msg.RequestID, msg.Approved) {
						break
					}
				}

			case "always_allow":
				entry := config.WhitelistEntry{
					Tool:           msg.Tool,
					Allow:          "always",
					CommandPattern: msg.CommandPattern,
				}
				wc.server.persistWhitelistEntry(entry)
				// Task 8 will extend persistWhitelistEntry to propagate to all
				// live hub brokers. For now it persists + updates server cfg.
				wc.mu.Lock()
				hubs := make([]*SessionHub, 0, len(wc.watching))
				for _, h := range wc.watching {
					hubs = append(hubs, h)
				}
				wc.mu.Unlock()
				for _, h := range hubs {
					if h.Respond(msg.RequestID, true) {
						break
					}
				}

			case "list_sessions":
				sessions, err := wc.store.ListSessions(ctx)
				if err != nil {
					wc.send(outMsg{Type: "error", Message: err.Error()})
					continue
				}
				if sessions == nil {
					sessions = []session.Session{}
				}
				wc.send(outMsg{Type: "sessions", Data: sessions})

			case "load_session":
				msgs, err := wc.store.GetMessagesWithResults(ctx, msg.SessionID)
				if err != nil {
					wc.send(outMsg{Type: "error", Message: err.Error()})
					continue
				}
				if msgs == nil {
					msgs = []session.Message{}
				}
				wc.send(outMsg{Type: "history", SessionID: msg.SessionID, Messages: msgs})
				sessions, _ := wc.store.ListSessions(ctx)
				if sessions == nil {
					sessions = []session.Session{}
				}
				wc.send(outMsg{Type: "sessions", Data: sessions})

				// Register this tab as a watcher for the loaded session. Hub.Watch
				// replays any pending approvals for that session.
				wc.watchSession(msg.SessionID)

			case "delete_session":
				wc.store.DeleteSession(ctx, msg.SessionID)
				sessions, _ := wc.store.ListSessions(ctx)
				wc.send(outMsg{Type: "sessions", Data: sessions})
			}

		case <-ctx.Done():
			return
		}
	}
}

type wsSubagentEvents struct {
	wc *wsConn
}

func (e *wsSubagentEvents) OnStart(sessionID, parentID, task string) {
	e.wc.sendCritical(outMsg{Type: "subagent_start", SessionID: sessionID, Data: parentID, Message: task})
}

func (e *wsSubagentEvents) OnToken(sessionID, token string) {
	e.wc.send(outMsg{Type: "subagent_token", SessionID: sessionID, Data: token})
}

func (e *wsSubagentEvents) OnThinkingToken(sessionID, token string) {
	e.wc.send(outMsg{Type: "subagent_thinking_token", SessionID: sessionID, Data: token})
}

func (e *wsSubagentEvents) OnToolResult(sessionID, tool, params, result string, approved bool) {
	e.wc.send(outMsg{
		Type:      "subagent_tool_result",
		SessionID: sessionID,
		Tool:      tool,
		Params:    json.RawMessage(params),
		Result:    result,
		Approved:  approved,
	})
}

func (e *wsSubagentEvents) OnDone(sessionID, finalText string) {
	e.wc.sendCritical(outMsg{Type: "subagent_done", SessionID: sessionID, Message: finalText})
}

func (e *wsSubagentEvents) OnError(sessionID, errMsg string) {
	e.wc.sendCritical(outMsg{Type: "subagent_error", SessionID: sessionID, Message: errMsg})
}

func (wc *wsConn) runAgent(ctx context.Context, sessionID, content string, steerCh chan string, isNew bool) {
	contextWindow := wc.server.cfg.ContextWindow
	hub := wc.server.getOrCreateHub(sessionID)
	hub.Retain()
	defer func() {
		hub.Release()
		wc.server.removeHubIfIdle(sessionID)
		wc.mu.Lock()
		wc.running = false
		wc.runCancel = nil
		wc.steerCh = nil
		wc.mu.Unlock()
	}()

	if wc.server.cfg.LLMClient == nil {
		return
	}

	spawnTool := agent.NewSpawnSubagentTool(
		wc.server.cfg.LLMClient,
		wc.server.cfg.Registry, // base registry: subagents do not get spawn_subagent, preventing recursion
		wc.server.cfg.Store,
		hub.Broker(),
		&wsSubagentEvents{wc: wc},
		wc.server.cfg.SubagentSystemPrompt,
		sessionID,
	)
	perRunReg := wc.server.cfg.Registry.CloneWith(spawnTool)
	loop := agent.NewLoop(wc.server.cfg.LLMClient, perRunReg, wc.server.cfg.Store, wc.server.cfg.SystemPrompt)

	onThinking := func(token string) {
		wc.send(outMsg{Type: "thinking_token", Data: token})
	}

	err := loop.Run(ctx, sessionID, wc.tabID, content, hub.Broker(),
		func(token string) { wc.send(outMsg{Type: "token", Data: token}) },
		func(errMsg string) { wc.send(outMsg{Type: "error", Message: errMsg}) },
		func(tool, params, result string, approved bool) {
			wc.send(outMsg{
				Type:     "tool_result",
				Tool:     tool,
				Params:   json.RawMessage(params),
				Result:   result,
				Approved: approved,
			})
		},
		steerCh,
		func(prompt, completion, total int) {
			wc.send(outMsg{
				Type:             "usage",
				PromptTokens:     prompt,
				CompletionTokens: completion,
				ContextWindow:    contextWindow,
			})
		},
		onThinking,
	)

	if err != nil {
		if ctx.Err() == nil {
			wc.send(outMsg{Type: "error", Message: err.Error()})
		} else {
			wc.send(outMsg{Type: "stopped"})
		}
		return
	}
	if ctx.Err() != nil {
		wc.send(outMsg{Type: "stopped"})
	}
	wc.send(outMsg{Type: "done", MessageID: ""})

	if isNew && wc.server.cfg.LLMClient != nil {
		go func() {
			tCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			titleLoop := agent.NewLoop(wc.server.cfg.LLMClient, wc.server.cfg.Registry, wc.server.cfg.Store, "")
			title, err := titleLoop.GenerateTitle(tCtx, content)
			if err != nil {
				log.Printf("GenerateTitle: %v", err)
				return
			}
			if err := wc.server.cfg.Store.UpdateSessionTitle(tCtx, sessionID, title); err != nil {
				log.Printf("UpdateSessionTitle: %v", err)
				return
			}
			sessions, _ := wc.server.cfg.Store.ListSessions(tCtx)
			if sessions == nil {
				sessions = []session.Session{}
			}
			wc.send(outMsg{Type: "sessions", Data: sessions})
		}()
	}
	if !isNew {
		sessions, _ := wc.server.cfg.Store.ListSessions(ctx)
		wc.send(outMsg{Type: "sessions", Data: sessions})
	}
}

func (wc *wsConn) send(msg outMsg) {
	select {
	case wc.outbound <- msg:
	default:
		log.Printf("outbound channel full, dropping message type=%s", msg.Type)
	}
}

// sendCritical is for low-volume lifecycle events whose loss breaks the UI
// (e.g., subagent_start — if dropped, the frontend never renders the block).
// Waits up to 100ms for space before dropping. Returns true if delivered.
func (wc *wsConn) sendCritical(msg outMsg) bool {
	select {
	case wc.outbound <- msg:
		return true
	case <-time.After(100 * time.Millisecond):
		log.Printf("CRITICAL: outbound channel full, dropping lifecycle message type=%s", msg.Type)
		return false
	}
}

// unwatchAll removes this tab from every hub it currently watches and
// asks the server to clean up any hub that becomes idle as a result.
func (wc *wsConn) unwatchAll() {
	wc.mu.Lock()
	hubs := make([]*SessionHub, 0, len(wc.watching))
	for _, h := range wc.watching {
		hubs = append(hubs, h)
	}
	wc.watching = make(map[string]*SessionHub)
	wc.mu.Unlock()

	for _, h := range hubs {
		h.Unwatch(wc.tabID)
		wc.server.removeHubIfIdle(h.sessionID)
	}
}

// watchSession atomically switches the tab's subscription to sessionID.
// If the tab was watching a different session, that hub is unwatched
// first; idempotent if already watching sessionID (Watch is idempotent).
func (wc *wsConn) watchSession(sessionID string) {
	hub := wc.server.getOrCreateHub(sessionID)

	wc.mu.Lock()
	var toUnwatch []*SessionHub
	for sid, h := range wc.watching {
		if sid == sessionID {
			continue
		}
		toUnwatch = append(toUnwatch, h)
		delete(wc.watching, sid)
	}
	wc.watching[sessionID] = hub
	wc.mu.Unlock()

	hub.Watch(wc)
	for _, h := range toUnwatch {
		h.Unwatch(wc.tabID)
		wc.server.removeHubIfIdle(h.sessionID)
	}
}
