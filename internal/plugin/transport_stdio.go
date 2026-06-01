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
	"strings"
	"sync"
)

// stdioTransport speaks newline-delimited JSON-RPC 2.0 over a subprocess's
// stdin/stdout — the MCP stdio convention (one JSON message per line, no
// embedded newlines). A dedicated reader goroutine owns stdout and demuxes each
// response to the waiting call by id, so a call can abandon a blocking read the
// moment its context is cancelled (the subprocess is bound to the session, not
// the turn, so a hung server would otherwise hang a cancelled turn forever).
// callMu serialises a request/response round-trip over the shared pipe.
type stdioTransport struct {
	name   string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr *tailBuffer

	callMu sync.Mutex // one in-flight request/response at a time over the shared pipe

	mu      sync.Mutex
	nextID  int
	pending map[int]chan rpcResponse
	readErr error // set once the reader goroutine exits; further calls fail fast

	waitOnce sync.Once
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
	stderr := &tailBuffer{limit: 16 * 1024}
	cmd.Stderr = stderr
	if s.Stderr != nil {
		cmd.Stderr = io.MultiWriter(stderr, s.Stderr)
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
	t := &stdioTransport{
		name:    s.Name,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReader(stdout),
		stderr:  stderr,
		pending: map[int]chan rpcResponse{},
	}
	go t.readLoop()
	return t, nil
}

// readLoop owns stdout for the transport's lifetime: it reads one JSON-RPC
// message per line, drops server-initiated notifications/requests (they carry a
// method), and hands each response to the call waiting on its id. On any read
// error it fails every pending call and exits.
func (t *stdioTransport) readLoop() {
	for {
		line, err := t.stdout.ReadBytes('\n')
		if err != nil {
			t.failAll(err)
			return
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var probe struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(line, &probe)
		if probe.Method != "" {
			continue // server notification/request, not a response to one of our calls
		}
		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue // unparseable line with no id — can't route it, skip
		}
		t.mu.Lock()
		ch := t.pending[resp.ID]
		delete(t.pending, resp.ID)
		t.mu.Unlock()
		if ch != nil {
			ch <- resp // buffered(1): never blocks, even if the caller already left
		}
	}
}

// failAll records the terminal read error and unblocks every pending call by
// closing its channel; a caller distinguishes this from a real response by the
// closed-channel receive.
func (t *stdioTransport) failAll(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.readErr == nil {
		t.readErr = err
	}
	for id, ch := range t.pending {
		close(ch)
		delete(t.pending, id)
	}
}

func (t *stdioTransport) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	t.callMu.Lock()
	defer t.callMu.Unlock()

	t.mu.Lock()
	if t.readErr != nil {
		t.mu.Unlock()
		return nil, t.withStderr(fmt.Errorf("plugin %q: read: %w", t.name, t.readErr))
	}
	t.nextID++
	id := t.nextID
	ch := make(chan rpcResponse, 1)
	t.pending[id] = ch
	t.mu.Unlock()

	defer func() {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
	}()

	if err := t.write(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		return nil, fmt.Errorf("plugin %q: write %s: %w", t.name, method, err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp, ok := <-ch:
		if !ok {
			return nil, t.withStderr(fmt.Errorf("plugin %q: read: %w", t.name, t.readErr))
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("plugin %q: %w", t.name, resp.Error)
		}
		return resp.Result, nil
	}
}

func (t *stdioTransport) notify(_ context.Context, method string, params any) error {
	return t.write(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
}

func (t *stdioTransport) write(v any) error {
	b, err := json.Marshal(v) // marshaled JSON never contains a literal newline
	if err != nil {
		return err
	}
	if _, err = t.stdin.Write(append(b, '\n')); err != nil {
		return t.withStderr(err)
	}
	return nil
}

func (t *stdioTransport) withStderr(err error) error {
	if t.stderr == nil {
		return err
	}
	t.wait() // reap the exited child so its stderr copy goroutine has flushed the tail
	msg := t.stderr.String()
	if msg == "" {
		return err
	}
	return fmt.Errorf("%w: stderr: %s", err, msg)
}

// wait reaps the child exactly once; cmd.Wait blocks until the stderr-copy
// goroutine completes, so the tail buffer is settled before anyone reads it.
func (t *stdioTransport) wait() {
	t.waitOnce.Do(func() {
		if t.cmd != nil && t.cmd.Process != nil {
			_ = t.cmd.Wait()
		}
	})
}

func (t *stdioTransport) close() {
	if t.stdin != nil {
		_ = t.stdin.Close()
	}
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
		t.wait()
	}
}

type tailBuffer struct {
	mu    sync.Mutex
	limit int
	buf   []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if b.limit > 0 && len(b.buf) > b.limit {
		b.buf = append([]byte(nil), b.buf[len(b.buf)-b.limit:]...)
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.buf))
}
