package main

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jhyoong/GoHome/gohome/internal/agent"
	"github.com/jhyoong/GoHome/gohome/internal/config"
	"github.com/jhyoong/GoHome/gohome/internal/guard"
	"github.com/jhyoong/GoHome/gohome/internal/llm"
	"github.com/jhyoong/GoHome/gohome/internal/llm/common"
	"github.com/jhyoong/GoHome/gohome/internal/session"
	"github.com/jhyoong/GoHome/gohome/internal/tools"
	"github.com/jhyoong/GoHome/gohome/internal/tui"
)

var (
	endpointName = flag.String("endpoint", "", "endpoint name override")
	modelName    = flag.String("model", "", "model override")
	yolo         = flag.Bool("yolo", false, "disable all approval prompts")
	resume       = flag.Bool("resume", false, "resume a past session")
)

// newSessionID generates an 8-char lowercase base32 session ID using crypto/rand.
func newSessionID() string {
	buf := make([]byte, 5) // 5 bytes -> 8 base32 chars (no padding)
	if _, err := rand.Read(buf); err != nil {
		panic("newSessionID: crypto/rand failed: " + err.Error())
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	return strings.ToLower(enc.EncodeToString(buf))
}

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

	// Structured logging (Task 12.4).
	logFile, err := setupLogging(home)
	if err != nil {
		// Non-fatal: fall back to stderr and continue.
		fmt.Fprintf(os.Stderr, "gohome: logging setup failed: %v (continuing without file log)\n", err)
	}
	slog.Info("gohome started", "cwd", cwd, "home", home, "yolo", *yolo, "resume", *resume)

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

	// Resolve endpoint.
	epName := *endpointName
	if epName == "" {
		epName = settings.DefaultEndpoint
	}
	endpoint, ok := settings.Endpoints[epName]
	if !ok {
		if epName == "" {
			fmt.Fprintf(os.Stderr, "gohome: no endpoint configured. Set defaultEndpoint in ~/.gohome/settings.json or use --endpoint.\n")
		} else {
			fmt.Fprintf(os.Stderr, "gohome: endpoint %q not found. Check ~/.gohome/settings.json.\n", epName)
		}
		os.Exit(1)
	}

	apiKey, err := config.ResolveAPIKey(endpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gohome: no API key for endpoint %q.\n", epName)
		fmt.Fprintf(os.Stderr, "  Set apiKey in settings.json or the environment variable named by apiKeyEnv.\n")
		os.Exit(1)
	}

	// Model override.
	if *modelName != "" {
		endpoint.DefaultModel = *modelName
	}

	// Build LLM client.
	client, err := llm.New(endpoint, apiKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gohome: cannot create LLM client: %v\n", err)
		os.Exit(1)
	}

	// Build whitelist.
	wl, err := guard.LoadWhitelist(
		filepath.Join(home, "whitelist.json"),
		filepath.Join(cwd, ".gohome", "whitelist.json"),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gohome: whitelist error: %v\n", err)
		os.Exit(1)
	}

	// Build frontend and guard.
	fe := tui.NewFrontend()
	g := guard.NewGuard(wl, fe)
	g.SetYolo(*yolo)

	// Build tools registry.
	registry := tools.NewRegistry()
	registry.Register(tools.ReadTool{})
	registry.Register(tools.WriteTool{})
	registry.Register(tools.EditTool{})
	registry.Register(tools.BashTool{})

	// Fresh session.
	sess := session.NewSession(newSessionID(), cwd, endpoint.DefaultModel, epName)
	writerPath := session.SessionPath(home, cwd, sess.ID, time.Now().UTC())

	writer, err := session.OpenWriter(writerPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gohome: cannot open session writer: %v\n", err)
		os.Exit(1)
	}

	writer.Emit(session.SessionStart{
		ID:        sess.ID,
		ParentID:  sess.ParentID,
		CWD:       cwd,
		Model:     endpoint.DefaultModel,
		Endpoint:  epName,
		Depth:     sess.Depth,
		StartedAt: sess.StartedAt,
	})

	// Build TUI model.
	m := tui.New(fe)
	m.SetModelName(endpoint.DefaultModel)
	contextWindow := endpoint.ContextWindow
	if contextWindow <= 0 {
		contextWindow = 128000
	}
	m.SetContextWindow(contextWindow)
	m.SetYolo(*yolo)

	// Build tea program and wire frontend.
	p := tea.NewProgram(m, tea.WithAltScreen())
	fe.SetProgram(p)

	// Build agent.
	const systemPrompt = `You are gohome, an AI coding assistant. You help users with software development tasks.
You have access to tools for reading and writing files, running bash commands, and spawning subagents for parallel work.
Be concise and precise. Ask for clarification when requirements are ambiguous.`

	a := &agent.Agent{
		Client:    client,
		Tools:     registry,
		Guard:     g,
		Frontend:  fe,
		Writer:    writer,
		System:    systemPrompt,
		MaxTokens: 4096,
		Home:      home,
		Session:   sess,
	}
	a.RegisterSubagentTool()

	// Agent driver goroutine: REPL loop awaiting user input.
	ctx := context.Background()
	go func() {
		for {
			text, err := fe.AwaitUserInput(ctx, sess.ID)
			if err != nil {
				return
			}
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
			if err := a.Run(ctx, sess); err != nil {
				slog.Error("agent run failed", "err", err)
			}
		}
	}()

	// Run TUI in the main goroutine.
	if _, err := p.Run(); err != nil {
		slog.Error("tui error", "err", err)
	}

	writer.Emit(session.SessionEnd{Reason: "user_quit"})
	_ = writer.Close()

	if logFile != nil {
		slog.Info("gohome exiting")
		_ = logFile.Close()
	}
}
