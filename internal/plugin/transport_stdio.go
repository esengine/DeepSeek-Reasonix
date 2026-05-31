package plugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

// stdioTransport speaks newline-delimited JSON-RPC 2.0 over a subprocess's
// stdin/stdout — the MCP stdio convention (one JSON message per line, no
// embedded newlines). The mutex serialises a request and its response on the
// shared pipe so concurrent tool calls don't interleave.
type stdioTransport struct {
	name   string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	mu     sync.Mutex
	nextID int
}

func newStdioTransport(ctx context.Context, s Spec) (*stdioTransport, error) {
	if s.Command == "" {
		return nil, fmt.Errorf("stdio plugin %q: command is required", s.Name)
	}
	cmd := exec.CommandContext(ctx, s.Command, s.Args...)
	cmd.Env = append(os.Environ(), envSlice(s.Env)...)
	if s.Dir != "" {
		cmd.Dir = s.Dir // pin cwd-aware servers (e.g. CodeGraph) to the project root
	}
	cmd.Stderr = os.Stderr // default: surface plugin logs to the terminal
	if s.Stderr != nil {
		cmd.Stderr = s.Stderr // caller-supplied override (e.g. io.Discard)
	}

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
	return &stdioTransport{name: s.Name, cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}, nil
}

func (t *stdioTransport) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.nextID++
	id := t.nextID
	if err := t.write(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		return nil, fmt.Errorf("plugin %q: write %s: %w", t.name, method, err)
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line, err := t.stdout.ReadBytes('\n')
		if err != nil {
			return nil, fmt.Errorf("plugin %q: read: %w", t.name, err)
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		// Skip server-initiated notifications/requests (they carry a method).
		var probe struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(line, &probe)
		if probe.Method != "" {
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			return nil, fmt.Errorf("plugin %q: decode response: %w", t.name, err)
		}
		if resp.ID != id {
			continue
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("plugin %q: %w", t.name, resp.Error)
		}
		return resp.Result, nil
	}
}

func (t *stdioTransport) notify(_ context.Context, method string, params any) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.write(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
}

func (t *stdioTransport) write(v any) error {
	b, err := json.Marshal(v) // marshaled JSON never contains a literal newline
	if err != nil {
		return err
	}
	_, err = t.stdin.Write(append(b, '\n'))
	return err
}

func (t *stdioTransport) close() {
	if t.stdin != nil {
		_ = t.stdin.Close()
	}
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
		_ = t.cmd.Wait()
	}
}
