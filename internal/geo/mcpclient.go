// Package geo provides GeoCode MCP client utilities for calling the Python MCP
// server from Go backend code (e.g., ReadFile geo preview generation).
package geo

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// MCPClient manages a subprocess connection to the geocode Python MCP server.
// It handles JSON-RPC 2.0 over stdio (newline-delimited).
type MCPClient struct {
	cmd    *exec.Cmd
	stdin  *json.Encoder
	stdout *bufio.Reader
	mu     sync.Mutex
	nextID int
}

// NewClient starts the geocode MCP server as a subprocess and performs the
// MCP initialize handshake. cmdArgs[0] = executable, cmdArgs[1:] = args.
func NewClient(cwd string, cmdArgs []string) (*MCPClient, error) {
	if len(cmdArgs) == 0 {
		return nil, fmt.Errorf("geo mcpclient: no command")
	}
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Dir = cwd
	// Redirect stderr to /dev/null (Python logging goes there, not JSON-RPC)
	// cmd.Stderr = nil means inherit — we set a discard pipe.
	cmd.Stderr = nil

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("geo mcpclient: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("geo mcpclient: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("geo mcpclient: start %s: %w", cmdArgs[0], err)
	}

	c := &MCPClient{
		cmd:    cmd,
		stdin:  json.NewEncoder(stdin),
		stdout: bufio.NewReader(stdout),
		nextID: 1,
	}

	// MCP initialize handshake
	initResp, err := c.call("initialize", rpcRequest{
		ProtocolVersion: "2024-11-05",
		Capabilities:    map[string]any{},
		ClientInfo:      map[string]string{"name": "rs-reasonix-geo", "version": "0.1.0"},
	})
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("geo mcpclient: initialize: %w", err)
	}
	_ = initResp // initialize succeeded

	return c, nil
}

type rpcRequest struct {
	ProtocolVersion string         `json:"protocolVersion,omitempty"`
	Capabilities    map[string]any `json:"capabilities,omitempty"`
	ClientInfo      map[string]string `json:"clientInfo,omitempty"`
}

type rpcCall struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// CallTool invokes a named tool on the MCP server with the given arguments.
func (c *MCPClient) CallTool(name string, args map[string]any, timeout time.Duration) (json.RawMessage, error) {
	return c.call("tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
}

func (c *MCPClient) call(method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.nextID
	c.nextID++

	req := rpcCall{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	if err := c.stdin.Encode(req); err != nil {
		return nil, fmt.Errorf("mcpclient write: %w", err)
	}

	// Read response — MCP over stdio is line-delimited JSON
	var resp rpcResponse
	line, err := c.stdout.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("mcpclient read: %w", err)
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("mcpclient parse: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("mcpclient rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
}

// Close terminates the subprocess.
func (c *MCPClient) Close() error {
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
	return nil
}
