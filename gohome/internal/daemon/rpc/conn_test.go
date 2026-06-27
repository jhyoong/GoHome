package rpc

import (
	"encoding/json"
	"net"
	"testing"
)

func setupConnPair(t *testing.T) (conn1, conn2 *Conn, cleanup func()) {
	t.Helper()
	c1, c2 := net.Pipe()
	cleanup = func() {
		c1.Close()
		c2.Close()
	}
	return NewConn(c1), NewConn(c2), cleanup
}

func TestConn_WriteAndRead(t *testing.T) {
	conn1, conn2, cleanup := setupConnPair(t)
	defer cleanup()

	req := Request{
		ID:     NewID(1),
		Method: "initialize",
		Params: json.RawMessage(`{"name":"test"}`),
	}

	// Write from conn1, read from conn2.
	errCh := make(chan error, 1)
	go func() {
		errCh <- conn1.WriteRequest(req)
	}()

	msg, err := conn2.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if werr := <-errCh; werr != nil {
		t.Fatalf("WriteRequest: %v", werr)
	}

	if !msg.IsRequest() {
		t.Fatal("expected IsRequest() to be true")
	}
	if msg.Method != "initialize" {
		t.Fatalf("method = %q, want \"initialize\"", msg.Method)
	}
	if msg.ID == nil {
		t.Fatal("expected non-nil ID")
	}
	if msg.ID.Int64() != 1 {
		t.Fatalf("id = %d, want 1", msg.ID.Int64())
	}
	if string(msg.Params) != `{"name":"test"}` {
		t.Fatalf("params = %s, want {\"name\":\"test\"}", msg.Params)
	}
}

func TestConn_WriteNotification(t *testing.T) {
	conn1, conn2, cleanup := setupConnPair(t)
	defer cleanup()

	errCh := make(chan error, 1)
	go func() {
		errCh <- conn1.Notify("notifications/initialized", nil)
	}()

	msg, err := conn2.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if werr := <-errCh; werr != nil {
		t.Fatalf("Notify: %v", werr)
	}

	if !msg.IsNotification() {
		t.Fatal("expected IsNotification() to be true")
	}
	if msg.IsRequest() {
		t.Fatal("expected IsRequest() to be false")
	}
	if msg.Method != "notifications/initialized" {
		t.Fatalf("method = %q, want \"notifications/initialized\"", msg.Method)
	}
	if msg.ID != nil {
		t.Fatal("expected nil ID for notification")
	}
}

func TestConn_WriteResponse(t *testing.T) {
	conn1, conn2, cleanup := setupConnPair(t)
	defer cleanup()

	resp := Response{
		ID:     NewID(7),
		Result: json.RawMessage(`{"tools":[]}`),
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- conn1.WriteResponse(resp)
	}()

	msg, err := conn2.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if werr := <-errCh; werr != nil {
		t.Fatalf("WriteResponse: %v", werr)
	}

	if !msg.IsResponse() {
		t.Fatal("expected IsResponse() to be true")
	}
	if msg.IsRequest() {
		t.Fatal("expected IsRequest() to be false")
	}
	if msg.ID == nil {
		t.Fatal("expected non-nil ID")
	}
	if msg.ID.Int64() != 7 {
		t.Fatalf("id = %d, want 7", msg.ID.Int64())
	}
	if string(msg.Result) != `{"tools":[]}` {
		t.Fatalf("result = %s, want {\"tools\":[]}", msg.Result)
	}
}
