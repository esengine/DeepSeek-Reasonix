// Package lsp is a minimal Language Server Protocol client: it spawns a language
// server per language on demand, syncs queried documents from disk, and adapts a
// few read-only capabilities (definition, references, hover, diagnostics) to the
// tool.Tool interface. Servers are not bundled — they resolve on PATH and a clear
// install hint is returned when one is missing.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("lsp error %d: %s", e.Code, e.Message) }

type outMsg struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method,omitempty"`
	Params  any    `json:"params,omitempty"`
	Result  any    `json:"result,omitempty"`
}

type inMsg struct {
	ID     *int64          `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
	Params json.RawMessage `json:"params"`
}

// conn speaks LSP framing (Content-Length headers) over a subprocess's
// stdin/stdout. A single read-pump goroutine demultiplexes the stream:
// id-bearing responses wake the matching call; method-only messages are
// notifications (diagnostics); id+method messages are server→client requests
// that must be answered or some servers stall during initialize.
type conn struct {
	w       io.Writer
	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan inMsg

	onNotify  func(method string, params json.RawMessage)
	onRequest func(id int64, method string, params json.RawMessage)

	closeOnce sync.Once
	closed    chan struct{}
	err       error
}

func newConn(w io.Writer, r io.Reader,
	onNotify func(string, json.RawMessage),
	onRequest func(int64, string, json.RawMessage)) *conn {
	c := &conn{
		w:         w,
		pending:   map[int64]chan inMsg{},
		onNotify:  onNotify,
		onRequest: onRequest,
		closed:    make(chan struct{}),
	}
	go c.readLoop(r)
	return c
}

func (c *conn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan inMsg, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if err := c.writeMsg(outMsg{JSONRPC: "2.0", ID: &id, Method: method, Params: params}); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, c.err
	case m := <-ch:
		if m.Error != nil {
			return nil, m.Error
		}
		return m.Result, nil
	}
}

func (c *conn) notify(method string, params any) error {
	return c.writeMsg(outMsg{JSONRPC: "2.0", Method: method, Params: params})
}

func (c *conn) reply(id int64, result any) error {
	return c.writeMsg(outMsg{JSONRPC: "2.0", ID: &id, Result: result})
}

func (c *conn) writeMsg(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(b)); err != nil {
		return err
	}
	_, err = c.w.Write(b)
	return err
}

func (c *conn) readLoop(r io.Reader) {
	br := bufio.NewReader(r)
	for {
		body, err := readFrame(br)
		if err != nil {
			c.fail(err)
			return
		}
		var m inMsg
		if json.Unmarshal(body, &m) != nil {
			continue
		}
		switch {
		case m.Method != "" && m.ID != nil:
			if c.onRequest != nil {
				c.onRequest(*m.ID, m.Method, m.Params)
			}
		case m.Method != "":
			if c.onNotify != nil {
				c.onNotify(m.Method, m.Params)
			}
		case m.ID != nil:
			c.mu.Lock()
			ch := c.pending[*m.ID]
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		}
	}
}

func (c *conn) fail(err error) {
	c.closeOnce.Do(func() {
		c.err = err
		close(c.closed)
	})
}

// maxFrameBytes caps a single LSP message body. Generous for any real response
// (document symbols, semantic tokens for a huge file) while stopping a corrupt or
// desynced Content-Length from triggering an unbounded allocation that OOMs the
// whole process.
const maxFrameBytes = 64 << 20 // 64 MiB

// readFrame reads one LSP message: header lines terminated by a blank line, then
// exactly Content-Length bytes of JSON body.
func readFrame(r *bufio.Reader) ([]byte, error) {
	n := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			parsed, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				return nil, fmt.Errorf("bad Content-Length %q: %w", v, err)
			}
			n = parsed
		}
	}
	if n < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	if n > maxFrameBytes {
		return nil, fmt.Errorf("Content-Length %d exceeds the %d-byte frame cap", n, maxFrameBytes)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
