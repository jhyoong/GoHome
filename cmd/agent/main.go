package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	gohome "github.com/JiaHui/gohome"
	"github.com/JiaHui/gohome/internal/agent"
	"github.com/JiaHui/gohome/internal/config"
	"github.com/JiaHui/gohome/internal/llm"
	"github.com/JiaHui/gohome/internal/mcp"
	"github.com/JiaHui/gohome/internal/server"
	"github.com/JiaHui/gohome/internal/session"
	"github.com/JiaHui/gohome/internal/tools"
)

var version = "dev"

func main() {
	var (
		configPath = flag.String("config", "~/.agent-chat/config.yaml", "Path to config file")
		port       = flag.Int("port", 0, "Override server port")
		host       = flag.String("host", "", "Override server host")
		dbPath     = flag.String("db", "", "Override database path")
		verbose    = flag.Bool("verbose", false, "Enable debug logging")
		showVer    = flag.Bool("version", false, "Print version and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("agent-chat", version)
		os.Exit(0)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Printf("INFO: config file not found at %s, using defaults", *configPath)
			cfg = &config.Config{}
			cfg.Server.Host = "127.0.0.1"
			cfg.Server.Port = 3000
		} else {
			log.Fatalf("loading config: %v", err)
		}
	}

	if *port != 0 {
		cfg.Server.Port = *port
	}
	if *host != "" {
		cfg.Server.Host = *host
	}
	if *dbPath != "" {
		cfg.Storage.Path = *dbPath
	}
	if cfg.Storage.Path == "" {
		home, _ := os.UserHomeDir()
		cfg.Storage.Path = filepath.Join(home, ".agent-chat", "data.db")
	}
	if cfg.Endpoint.URL == "" {
		cfg.Endpoint.URL = "http://localhost:8080/v1"
	}

	if *verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
		log.Printf("DEBUG: config loaded: endpoint=%s model=%s", cfg.Endpoint.URL, cfg.Endpoint.Model)
	}

	if cfg.Server.Host == "0.0.0.0" || cfg.Server.Host == "::" {
		log.Println("WARNING: Server is listening on all interfaces with no authentication.")
		log.Println("WARNING: Any device on your network can access this agent and execute tools.")
	}

	if err := os.MkdirAll(filepath.Dir(cfg.Storage.Path), 0755); err != nil {
		log.Fatalf("creating storage dir: %v", err)
	}
	store, err := session.Open(cfg.Storage.Path)
	if err != nil {
		log.Fatalf("opening database: %v", err)
	}
	defer store.Close()

	reg := tools.NewRegistry()
	reg.Register(&tools.ShellTool{})
	reg.Register(&tools.FileReadTool{})
	reg.Register(&tools.FileWriteTool{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mcpConns := mcp.ConnectAll(ctx, cfg.MCPServers, reg)
	defer mcp.CloseAll(mcpConns)

	llmClient := llm.NewClient(cfg.Endpoint)
	loop := agent.NewLoop(llmClient, reg, store, cfg.SystemPrompt)

	srv := server.New(server.Config{
		Store:    store,
		Loop:     loop,
		Approval: cfg.Approval,
	})

	staticFS, err := fs.Sub(gohome.WebStatic, "web/static")
	if err != nil {
		log.Fatalf("embed sub: %v", err)
	}

	apiHandler := srv.Handler()
	mux := http.NewServeMux()
	mux.Handle("/api/", apiHandler)
	mux.Handle("/ws", apiHandler)
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	httpSrv := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler: mux,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("shutting down...")
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		httpSrv.Shutdown(shutdownCtx)
	}()

	ln, err := net.Listen("tcp", httpSrv.Addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	log.Printf("agent-chat listening on http://%s", httpSrv.Addr)
	if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Printf("server error: %v", err)
	}
}
