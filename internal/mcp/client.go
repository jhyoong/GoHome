package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"

	"github.com/JiaHui/gohome/internal/config"
	"github.com/JiaHui/gohome/internal/tools"
)

type Connection struct {
	name      string
	transport string
	stdin     io.WriteCloser
	stdout    *bufio.Reader
	cmd       *exec.Cmd
	sseURL    string
	nextID    int
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type MCPTool struct {
	serverName  string
	toolName    string
	description string
	parameters  json.RawMessage
	conn        *Connection
}

func (t *MCPTool) Name() string                { return t.serverName + "." + t.toolName }
func (t *MCPTool) Description() string         { return t.description }
func (t *MCPTool) Parameters() json.RawMessage { return t.parameters }

func (t *MCPTool) Execute(ctx context.Context, params json.RawMessage) (string, error) {
	return t.conn.callTool(t.toolName, params)
}

func (c *Connection) send(method string, params any) (json.RawMessage, error) {
	c.nextID++
	req := rpcRequest{JSONRPC: "2.0", ID: c.nextID, Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	switch c.transport {
	case "stdio":
		data = append(data, '\n')
		if _, err := c.stdin.Write(data); err != nil {
			return nil, fmt.Errorf("write to MCP: %w", err)
		}
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("read from MCP: %w", err)
		}
		var resp rpcResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			return nil, err
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("MCP error: %s", resp.Error.Message)
		}
		return resp.Result, nil
	case "sse":
		r, err := http.Post(c.sseURL, "application/json", bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer r.Body.Close()
		var resp rpcResponse
		if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
			return nil, err
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("MCP error: %s", resp.Error.Message)
		}
		return resp.Result, nil
	default:
		return nil, fmt.Errorf("unknown transport %q", c.transport)
	}
}

func (c *Connection) initialize() error {
	_, err := c.send("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "agent-chat", "version": "1.0"},
	})
	return err
}

func (c *Connection) listTools() ([]struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}, error) {
	result, err := c.send("tools/list", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return resp.Tools, nil
}

func (c *Connection) callTool(name string, params json.RawMessage) (string, error) {
	var p map[string]any
	if err := json.Unmarshal(params, &p); err != nil {
		return "", err
	}
	result, err := c.send("tools/call", map[string]any{"name": name, "arguments": p})
	if err != nil {
		return "", err
	}
	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return string(result), nil
	}
	var out string
	for _, item := range resp.Content {
		if item.Type == "text" {
			out += item.Text
		}
	}
	return out, nil
}

func (c *Connection) Close() {
	if c.cmd != nil {
		c.stdin.Close()
		c.cmd.Wait()
	}
}

func connectStdio(cfg config.MCPServer) (*Connection, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	conn := &Connection{
		name: cfg.Name, transport: "stdio",
		stdin: stdin, stdout: bufio.NewReader(stdout), cmd: cmd,
	}
	if err := conn.initialize(); err != nil {
		cmd.Process.Kill()
		return nil, err
	}
	return conn, nil
}

func connectSSE(cfg config.MCPServer) (*Connection, error) {
	conn := &Connection{name: cfg.Name, transport: "sse", sseURL: cfg.URL}
	if err := conn.initialize(); err != nil {
		return nil, err
	}
	return conn, nil
}

func ConnectAll(_ context.Context, servers []config.MCPServer, reg *tools.Registry) []*Connection {
	var conns []*Connection
	for _, srv := range servers {
		var (
			conn *Connection
			err  error
		)
		switch srv.Transport {
		case "stdio":
			conn, err = connectStdio(srv)
		case "sse":
			conn, err = connectSSE(srv)
		default:
			log.Printf("WARNING: MCP server %q has unknown transport %q", srv.Name, srv.Transport)
			continue
		}
		if err != nil {
			log.Printf("WARNING: MCP server %q connect failed: %v", srv.Name, err)
			continue
		}

		toolList, err := conn.listTools()
		if err != nil {
			log.Printf("WARNING: MCP server %q list tools failed: %v", srv.Name, err)
			conn.Close()
			continue
		}
		for _, t := range toolList {
			mt := &MCPTool{
				serverName: srv.Name, toolName: t.Name,
				description: t.Description, parameters: t.InputSchema, conn: conn,
			}
			if err := reg.Register(mt); err != nil {
				log.Printf("WARNING: MCP tool %q from %q skipped: %v", t.Name, srv.Name, err)
			}
		}
		conns = append(conns, conn)
	}
	return conns
}

func CloseAll(conns []*Connection) {
	for _, c := range conns {
		c.Close()
	}
}
