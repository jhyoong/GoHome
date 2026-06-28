package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/daemon"
	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/llm"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
	"github.com/jhyoong/GoHome/gohome/internal/tui"
)

var version = "dev"

var (
	modelFlag   = flag.String("model", "", "model config name override")
	yolo        = flag.Bool("yolo", false, "disable all approval prompts")
	resume      = flag.Bool("resume", false, "resume a past session")
	showVersion = flag.Bool("version", false, "print version and exit")
	stopFlag    = flag.Bool("stop", false, "stop the running daemon and exit")
)

// setupLogging configures the global slog logger to write JSON to
// <home>/.gohome/logs/<YYYY-MM-DD>.log. Returns the open log file so the
// caller can close it on shutdown. If GOHOME_DEBUG=1 the level is Debug.
func setupLogging(home string) (*os.File, error) {
	logDir := filepath.Join(home, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("setupLogging: mkdir %s: %w", logDir, err)
	}

	today := time.Now().UTC().Format("2006-01-02")
	logPath := filepath.Join(logDir, today+".log")

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("setupLogging: open %s: %w", logPath, err)
	}

	level := slog.LevelInfo
	if os.Getenv("GOHOME_DEBUG") == "1" {
		level = slog.LevelDebug
	}

	h := slog.NewJSONHandler(f, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h))
	return f, nil
}

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Println("gohome " + version)
		return
	}

	// Resolve home and cwd before anything else.
	userHome, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gohome: cannot determine home directory: %v\n", err)
		os.Exit(1)
	}
	home := filepath.Join(userHome, ".gohome")

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gohome: cannot determine working directory: %v\n", err)
		os.Exit(1)
	}

	// Structured logging.
	logFile, err := setupLogging(home)
	if err != nil {
		// Non-fatal: fall back to stderr and continue.
		fmt.Fprintf(os.Stderr, "gohome: logging setup failed: %v (continuing without file log)\n", err)
	}
	slog.Info("gohome started", "cwd", cwd, "home", home, "yolo", *yolo, "resume", *resume)

	sockPath := filepath.Join(home, "daemon.sock")

	if *stopFlag {
		stopDaemon(sockPath)
		if logFile != nil {
			logFile.Close()
		}
		return
	}

	// Warn about --resume being deferred to Task 13.
	if *resume {
		fmt.Fprintf(os.Stderr, "gohome: --resume is not yet supported in daemon mode (will be added in a future update)\n")
	}

	// Load config.
	globalCfgPath, err := config.DefaultGlobalPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gohome: cannot determine global config path: %v\n", err)
		os.Exit(1)
	}
	settings, err := config.Load(globalCfgPath, config.DefaultProjectPath(cwd))
	if err != nil {
		fmt.Fprintf(os.Stderr, "gohome: config load error: %v\n", err)
		os.Exit(1)
	}

	// Resolve model config.
	cfgName := *modelFlag
	if cfgName == "" {
		cfgName = settings.DefaultModel
	}
	mc, ok := settings.ModelConfig[cfgName]
	if !ok {
		if cfgName == "" {
			fmt.Fprintf(os.Stderr, "gohome: no model configured. Set defaultModel in ~/.gohome/settings.json or use --model.\n")
		} else {
			fmt.Fprintf(os.Stderr, "gohome: model config %q not found. Check ~/.gohome/settings.json.\n", cfgName)
		}
		os.Exit(1)
	}

	// Check for existing daemon.
	if !isDaemonRunning(sockPath) {
		if err := startDaemon(sockPath, home, cwd, settings, mc, cfgName); err != nil {
			fmt.Fprintf(os.Stderr, "gohome: daemon start failed: %v\n", err)
			os.Exit(1)
		}
		waitForDaemon(sockPath, 5*time.Second)
	}

	// Run TUI client.
	runClient(sockPath, settings, mc)

	if logFile != nil {
		slog.Info("gohome exiting")
		_ = logFile.Close()
	}
}

// isDaemonRunning probes the daemon socket with a health check.
func isDaemonRunning(sockPath string) bool {
	conn, err := net.DialTimeout("unix", sockPath, time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(time.Second))

	c := rpc.NewConn(conn)
	if err := c.WriteRequest(rpc.Request{
		ID:     rpc.NewID(1),
		Method: rpc.MethodDaemonHealth,
		Params: json.RawMessage(`{}`),
	}); err != nil {
		return false
	}

	// Skip notifications (e.g. session.state sent on connect) until
	// we receive the health response.
	for {
		msg, err := c.Read()
		if err != nil {
			return false
		}
		if msg.IsResponse() {
			return msg.Error == nil
		}
	}
}

// waitForDaemon polls the socket until the daemon responds to a health check
// or the timeout elapses.
func waitForDaemon(sockPath string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isDaemonRunning(sockPath) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Fprintf(os.Stderr, "gohome: daemon did not start within %s\n", timeout)
	os.Exit(1)
}

// stopDaemon sends a daemon.stop RPC to the daemon and exits.
func stopDaemon(sockPath string) {
	conn, err := net.DialTimeout("unix", sockPath, time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gohome: no daemon running\n")
		return
	}
	defer conn.Close()

	c := rpc.NewConn(conn)
	_ = c.WriteRequest(rpc.Request{
		ID:     rpc.NewID(1),
		Method: rpc.MethodDaemonStop,
		Params: json.RawMessage(`{}`),
	})
	fmt.Println("gohome: daemon stopped")
}

// startDaemon builds all agent dependencies and starts the daemon server
// in-process as a goroutine.
func startDaemon(sockPath, home, cwd string, settings config.Settings, mc config.ModelConfig, cfgName string) error {
	apiKey, err := config.ResolveAPIKey(mc)
	if err != nil {
		return fmt.Errorf("no API key for model config %q", cfgName)
	}

	client, err := llm.New(mc, apiKey)
	if err != nil {
		return fmt.Errorf("cannot create LLM client: %w", err)
	}

	wl, err := guard.LoadWhitelist(
		filepath.Join(home, "whitelist.json"),
		filepath.Join(cwd, ".gohome", "whitelist.json"),
	)
	if err != nil {
		return fmt.Errorf("whitelist error: %w", err)
	}

	// Build guard with nil frontend. The daemon's RPCFrontend will be set
	// when the first client connects (server.go sets agent.Frontend = fe,
	// and the guard's frontend will be wired in a follow-up task).
	g := guard.NewGuard(wl, nil)
	g.SetYolo(*yolo)

	registry := tools.NewRegistry()
	registry.Register(tools.ReadTool{})
	registry.Register(tools.WriteTool{})
	registry.Register(tools.EditTool{})
	registry.Register(tools.BashTool{
		DefaultTimeoutMs: settings.BashTimeoutMs,
		MaxTimeoutMs:     settings.MaxBashTimeoutMs,
	})

	systemPrompt := `You are gohome, an AI coding assistant. You help users with software development tasks.
You have access to tools for reading and writing files, running bash commands, and spawning subagents for parallel work.
Be concise and precise. Ask for clarification when requirements are ambiguous.`
	if settings.SystemPrompt != "" {
		systemPrompt = settings.SystemPrompt
	}

	maxTokens := mc.MaxTokens
	if maxTokens <= 0 {
		maxTokens = config.DefaultMaxTokens
	}
	thinkingBudget := mc.ThinkingBudget
	if thinkingBudget <= 0 {
		thinkingBudget = config.DefaultThinkingBudget
	}

	sessionID := session.NewID()

	srv, err := daemon.NewServer(sockPath, daemon.ServerConfig{
		Version:        version,
		LLMClient:      client,
		Guard:          g,
		Registry:       registry,
		SystemPrompt:   systemPrompt,
		MaxTokens:      maxTokens,
		ThinkingBudget: thinkingBudget,
		Home:           home,
		CWD:            cwd,
		SessionID:      sessionID,
		Settings:       settings,
		ModelConfig:    cfgName,
		ModelName:      mc.ModelName,
	})
	if err != nil {
		return fmt.Errorf("cannot start daemon: %w", err)
	}

	go srv.Serve()
	return nil
}

// runClient connects to the daemon and runs the TUI.
func runClient(sockPath string, settings config.Settings, mc config.ModelConfig) {
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gohome: cannot connect to daemon: %v\n", err)
		os.Exit(1)
	}

	c := rpc.NewConn(conn)
	eventCh := make(chan tui.AgentEventMsg, 64)
	cfe := tui.NewClientFrontend(c, eventCh)

	go func() {
		cfe.ReadLoop()
		close(eventCh)
	}()

	// Build TUI model.
	m := tui.New("main")
	m.SetClientFrontend(cfe)
	m.SetModelName(mc.ModelName)

	contextWindow := mc.ContextWindow
	if contextWindow <= 0 {
		contextWindow = config.DefaultContextWindow
	}
	m.SetContextWindow(contextWindow)
	m.SetYolo(*yolo)

	warnPct := settings.ContextWarnPct
	if warnPct <= 0 {
		warnPct = config.DefaultContextWarnPct
	}
	critPct := settings.ContextCritPct
	if critPct <= 0 {
		critPct = config.DefaultContextCritPct
	}
	if warnPct >= critPct {
		warnPct = config.DefaultContextWarnPct
		critPct = config.DefaultContextCritPct
	}
	m.SetContextThresholds(warnPct, critPct)
	m.SetRenderThrottleMs(settings.RenderThrottleMs)
	m.SetSettings(settings)

	// Wire slash command callbacks to send RPC requests to the daemon.
	m.SetSlashCallbacks(tui.SlashCallbacks{
		ListSessions: func() ([]session.Listing, error) {
			return cfe.SendSessionList()
		},
		NewSession: func() (string, error) {
			return cfe.SendSessionNew()
		},
		ResumeSession: func(id string) ([]common.Message, error) {
			_, history, err := cfe.SendSessionResume(id)
			return history, err
		},
		CancelSession: func(id string) {
			go cfe.SendCancel(id)
		},
		SetModel: func(name string) (string, int, error) {
			return cfe.SendModelSet(name)
		},
	})

	p := tea.NewProgram(m, tea.WithAltScreen())

	// Feed agent events from the daemon ReadLoop into Bubble Tea.
	go func() {
		for ev := range eventCh {
			p.Send(ev)
		}
	}()

	// Feed approval requests from the daemon into Bubble Tea.
	go func() {
		for areq := range cfe.Approvals() {
			p.Send(areq)
		}
	}()

	// Feed state sync messages from the daemon into Bubble Tea.
	go func() {
		for ss := range cfe.StateSync() {
			p.Send(ss)
		}
	}()

	// Handle signals.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cfe.Close()
		p.Quit()
	}()

	if _, err := p.Run(); err != nil {
		slog.Error("tui error", "err", err)
	}

	signal.Stop(sigCh)
	_ = cfe.Close()
}
