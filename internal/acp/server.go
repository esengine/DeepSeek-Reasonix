package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
)

// maxMessageBytes caps a single inbound NDJSON line. ACP messages can embed
// resource text, so the limit is generous; a line past it is a framing error.
const maxMessageBytes = 32 << 20 // 32 MiB

// RequestHandler answers an inbound JSON-RPC request. The returned value is
// marshaled as the response result. To control the error code, return a
// *RPCError; any other error becomes ErrInternal.
type RequestHandler func(ctx context.Context, params json.RawMessage) (any, error)

// responseWithAfter lets a request handler schedule work that must run only
// after its successful JSON-RPC response has been written to the wire.
type responseWithAfter interface {
	Response() any
	AfterResponse()
}

// NotificationHandler reacts to an inbound notification. It cannot reply, so it
// returns nothing — errors have nowhere to go on the wire (stderr would corrupt
// stdout, which is the JSON-RPC channel).
type NotificationHandler func(ctx context.Context, params json.RawMessage)

// RPCError lets a handler choose the JSON-RPC error code returned to the client.
type RPCError struct {
	Code    int
	Message string
}

func (e *RPCError) Error() string { return e.Message }

// Conn is one NDJSON JSON-RPC 2.0 connection over a reader/writer pair (stdin/
// stdout in production). It dispatches inbound requests and notifications to
// registered handlers, and can itself send outbound notifications (session/update)
// and requests (session/request_permission), correlating replies by id.
//
// Writes are serialized by a mutex, so handlers running on separate goroutines
// (a long session/prompt alongside a session/cancel) never interleave a line.
// It implements notifier, the dependency the dispatch sink takes.
type Conn struct {
	r io.Reader

	wmu sync.Mutex
	enc *json.Encoder

	nextID atomic.Int64

	pmu     sync.Mutex
	pending map[int64]chan rpcResult

	reqH map[string]RequestHandler
	notH map[string]NotificationHandler

	wg        sync.WaitGroup
	closeOnce sync.Once
	closed    chan struct{}
}

// rpcResult is the outcome of an outbound request, delivered to the waiter.
type rpcResult struct {
	result json.RawMessage
	err    error
}

// rpcError is the JSON-RPC error object on the wire.
type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// outbound is a JSON-RPC frame we send. omitempty fields select between request,
// notification, success response, and error response shapes.
type outbound struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// inbound is a parsed JSON-RPC frame we received. The combination of id/method
// presence distinguishes request, notification, and response.
type inbound struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

// NewConn wires a connection over r (inbound) and w (outbound). Register handlers
// with Handle / HandleNotify before calling Serve. The encoder disables HTML
// escaping so payloads match main's JSON.stringify output byte-for-byte.
func NewConn(r io.Reader, w io.Writer) *Conn {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &Conn{
		r:       r,
		enc:     enc,
		pending: make(map[int64]chan rpcResult),
		reqH:    make(map[string]RequestHandler),
		notH:    make(map[string]NotificationHandler),
		closed:  make(chan struct{}),
	}
}

// Handle registers a request handler for method. Not safe to call concurrently
// with Serve; wire all handlers up first.
func (c *Conn) Handle(method string, h RequestHandler) { c.reqH[method] = h }

// HandleNotify registers a notification handler for method.
func (c *Conn) HandleNotify(method string, h NotificationHandler) { c.notH[method] = h }

// Serve reads inbound frames until the reader ends or ctx is cancelled. Each
// inbound request/notification runs on its own goroutine so a long-running prompt
// does not block cancellation or permission replies. When the read loop ends it
// cancels in-flight handlers (so prompts abort) and waits for them to return —
// flushing fast responses and unwinding aborted ones — before failing any
// outstanding outbound requests. Returns nil on clean EOF.
func (c *Conn) Serve(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	br := bufio.NewReaderSize(c.r, 64<<10)
	var loopErr error
	for {
		line, err := readLine(br)
		if len(line) > 0 {
			c.dispatch(ctx, line)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				loopErr = err
			}
			break
		}
		if err := ctx.Err(); err != nil {
			loopErr = err
			break
		}
	}

	cancel()     // abort in-flight handlers (prompts unwind via ctx)
	c.wg.Wait()  // let them flush their responses before we tear down
	c.shutdown() // fail any still-pending outbound requests
	return loopErr
}

// readLine reads one NDJSON line (without the trailing newline), enforcing the
// size cap. It returns the line and any read error; on EOF it still returns the
// trailing partial line so a final newline-less frame is processed.
func readLine(br *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := br.ReadSlice('\n')
		buf = append(buf, chunk...)
		if len(buf) > maxMessageBytes {
			return nil, errors.New("acp: message exceeds size limit")
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		// Trim the trailing newline (and CR) if present.
		n := len(buf)
		for n > 0 && (buf[n-1] == '\n' || buf[n-1] == '\r') {
			n--
		}
		return trimSpace(buf[:n]), err
	}
}

// trimSpace drops leading/trailing ASCII whitespace without allocating.
func trimSpace(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && isSpace(b[i]) {
		i++
	}
	for j > i && isSpace(b[j-1]) {
		j--
	}
	return b[i:j]
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// dispatch parses one frame and routes it. Requests and notifications fan out to
// goroutines; responses resolve inline (they are cheap and need ordering only
// against the pending map, which is mutex-guarded).
func (c *Conn) dispatch(ctx context.Context, line []byte) {
	var in inbound
	if err := json.Unmarshal(line, &in); err != nil {
		c.writeError(json.RawMessage("null"), ErrParse, "parse error")
		return
	}
	hasID := len(in.ID) > 0
	switch {
	case in.Method != "" && hasID:
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.serveRequest(ctx, in.ID, in.Method, in.Params)
		}()
	case in.Method != "" && !hasID:
		if h := c.notH[in.Method]; h != nil {
			c.wg.Add(1)
			go func() {
				defer c.wg.Done()
				h(ctx, in.Params)
			}()
		}
	case in.Method == "" && hasID:
		c.resolve(in)
	default:
		c.writeError(json.RawMessage("null"), ErrInvalidRequest, "invalid request")
	}
}

// serveRequest runs a request handler and writes its response (or error).
func (c *Conn) serveRequest(ctx context.Context, id json.RawMessage, method string, params json.RawMessage) {
	h := c.reqH[method]
	if h == nil {
		c.writeError(id, ErrMethodNotFound, "method not found: "+method)
		return
	}
	result, err := h(ctx, params)
	if err != nil {
		code := ErrInternal
		var re *RPCError
		if errors.As(err, &re) {
			code = re.Code
		}
		c.writeError(id, code, err.Error())
		return
	}
	var after func()
	if r, ok := result.(responseWithAfter); ok {
		result = r.Response()
		after = r.AfterResponse
	}
	raw, err := json.Marshal(result)
	if err != nil {
		c.writeError(id, ErrInternal, "marshal result: "+err.Error())
		return
	}
	if err := c.write(outbound{JSONRPC: "2.0", ID: id, Result: raw}); err != nil {
		return
	}
	if after != nil {
		after()
	}
}

// resolve delivers a response to the goroutine waiting on its outbound request.
func (c *Conn) resolve(in inbound) {
	id, err := strconv.ParseInt(string(in.ID), 10, 64)
	if err != nil {
		return // we only issue integer ids; an unparsable id isn't ours
	}
	c.pmu.Lock()
	ch := c.pending[id]
	delete(c.pending, id)
	c.pmu.Unlock()
	if ch == nil {
		return
	}
	if in.Error != nil {
		ch <- rpcResult{err: errors.New(in.Error.Message)}
		return
	}
	ch <- rpcResult{result: in.Result}
}

// Notify sends a fire-and-forget notification. Satisfies notifier.
func (c *Conn) Notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.write(outbound{JSONRPC: "2.0", Method: method, Params: raw})
}

// Request sends an outbound request and blocks until the peer responds, ctx is
// cancelled, or the connection closes. Satisfies notifier.
func (c *Conn) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	id := c.nextID.Add(1)
	ch := make(chan rpcResult, 1)
	c.pmu.Lock()
	c.pending[id] = ch
	c.pmu.Unlock()
	defer func() {
		c.pmu.Lock()
		delete(c.pending, id)
		c.pmu.Unlock()
	}()

	idRaw, _ := json.Marshal(id)
	if err := c.write(outbound{JSONRPC: "2.0", ID: idRaw, Method: method, Params: raw}); err != nil {
		return nil, err
	}
	select {
	case res := <-ch:
		return res.result, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, errors.New("acp: connection closed")
	}
}

func (c *Conn) write(m outbound) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.enc.Encode(m)
}

func (c *Conn) writeError(id json.RawMessage, code int, msg string) {
	_ = c.write(outbound{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}

// shutdown fails every in-flight outbound request so their goroutines unblock.
func (c *Conn) shutdown() {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.pmu.Lock()
		for id, ch := range c.pending {
			ch <- rpcResult{err: errors.New("acp: connection closed")}
			delete(c.pending, id)
		}
		c.pmu.Unlock()
	})
}
