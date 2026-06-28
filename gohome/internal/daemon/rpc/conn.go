package rpc

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"sync"
)

// Conn wraps a net.Conn for reading and writing newline-delimited JSON-RPC
// messages.
type Conn struct {
	conn    net.Conn
	scanner *bufio.Scanner
	mu      sync.Mutex // serializes writes
}

// NewConn creates a Conn that reads and writes JSON-RPC messages over c using
// newline-delimited JSON framing.
func NewConn(c net.Conn) *Conn {
	s := bufio.NewScanner(c)
	s.Buffer(make([]byte, 0, 64*1024), 1<<20)
	return &Conn{
		conn:    c,
		scanner: s,
	}
}

// Read blocks until a complete newline-delimited JSON-RPC message arrives and
// returns the decoded Message. It returns net.ErrClosed on EOF.
func (c *Conn) Read() (*Message, error) {
	if !c.scanner.Scan() {
		if err := c.scanner.Err(); err != nil {
			return nil, err
		}
		return nil, net.ErrClosed
	}
	return Decode(c.scanner.Bytes())
}

// WriteRequest marshals req as JSON and writes it as a single newline-
// terminated line.
func (c *Conn) WriteRequest(req Request) error {
	return c.writeJSON(req)
}

// WriteResponse marshals resp as JSON and writes it as a single newline-
// terminated line.
func (c *Conn) WriteResponse(resp Response) error {
	return c.writeJSON(resp)
}

// Notify sends a JSON-RPC notification (a Request without an ID).
func (c *Conn) Notify(method string, params json.RawMessage) error {
	return c.WriteRequest(Request{
		Method: method,
		Params: params,
	})
}

// Close closes the underlying connection.
func (c *Conn) Close() error {
	return c.conn.Close()
}

// writeJSON marshals v, appends a newline, and writes the result to the
// underlying connection. Writes are serialized with mu.
func (c *Conn) writeJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	c.mu.Lock()
	defer c.mu.Unlock()

	_, err = c.conn.Write(data)
	if err != nil {
		// Map common closed-connection errors to net.ErrClosed for
		// consistent caller handling.
		if isClosedErr(err) {
			return net.ErrClosed
		}
		return err
	}
	return nil
}

// isClosedErr returns true if the error indicates a closed connection or pipe.
func isClosedErr(err error) bool {
	return err == io.ErrClosedPipe || err == net.ErrClosed
}
