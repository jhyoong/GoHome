package daemon

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jhyoong/GoHome/gohome/internal/daemon/rpc"
)

// helper: send a JSON-RPC request over a raw connection and read the response.
func sendRequest(t *testing.T, conn net.Conn, id int64, method string, params json.RawMessage) *rpc.Message {
	t.Helper()

	req := rpc.Request{
		ID:     rpc.NewID(id),
		Method: method,
		Params: params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	data = append(data, '\n')

	if _, err := conn.Write(data); err != nil {
		t.Fatalf("write request: %v", err)
	}

	// Read response (newline-delimited).
	rc := rpc.NewConn(conn)
	msg, err := rc.Read()
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return msg
}

func TestServer_HealthCheck(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")

	srv, err := NewServer(sock, ServerConfig{Version: "test"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Start Serve in a goroutine.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		srv.Serve()
	}()

	// Give the server a moment to start accepting.
	time.Sleep(50 * time.Millisecond)

	// Connect to the server.
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send daemon.health request.
	msg := sendRequest(t, conn, 1, rpc.MethodDaemonHealth, nil)

	if msg.Error != nil {
		t.Fatalf("unexpected error: %v", msg.Error)
	}
	if msg.Result == nil {
		t.Fatal("expected result, got nil")
	}

	var health rpc.HealthResult
	if err := json.Unmarshal(msg.Result, &health); err != nil {
		t.Fatalf("unmarshal health: %v", err)
	}

	if health.Version != "test" {
		t.Errorf("version = %q, want %q", health.Version, "test")
	}
	if health.UptimeSeconds < 0 {
		t.Errorf("uptimeSeconds = %d, want >= 0", health.UptimeSeconds)
	}

	// Stop the server.
	srv.Stop()
	wg.Wait()

	// Verify socket file is cleaned up.
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Errorf("socket file still exists after stop")
	}
}

func TestServer_Stop(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")

	srv, err := NewServer(sock, ServerConfig{Version: "test"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Track when Serve exits.
	serveDone := make(chan struct{})
	go func() {
		srv.Serve()
		close(serveDone)
	}()

	// Give the server a moment to start accepting.
	time.Sleep(50 * time.Millisecond)

	// Connect and send daemon.stop.
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	msg := sendRequest(t, conn, 1, rpc.MethodDaemonStop, nil)
	if msg.Error != nil {
		t.Fatalf("unexpected error: %v", msg.Error)
	}

	// Verify Serve() returns within 2s.
	select {
	case <-serveDone:
		// Success.
	case <-time.After(2 * time.Second):
		t.Fatal("Serve() did not return within 2s after daemon.stop")
	}

	// Verify socket file is cleaned up.
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Errorf("socket file still exists after stop")
	}
}
