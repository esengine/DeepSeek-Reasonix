package rpcwire

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// RequestHandler answers an inbound JSON-RPC request.
type RequestHandler func(ctx context.Context, params json.RawMessage) (any, error)

// HandlerResponse lets a protocol perform transport-local cleanup only after a
// successful response write. The callback runs exactly once with the result
// frame's write error (nil on success). It must be fast and must not write to
// the same Conn. This is intentionally transport-neutral: for example, a
// protocol can acknowledge detach before releasing its connection ownership.
type HandlerResponse struct {
	Result     any
	AfterWrite func(error)
}

// RespondThen wraps a handler result with an after-write callback.
func RespondThen(result any, afterWrite func(error)) HandlerResponse {
	return HandlerResponse{Result: result, AfterWrite: afterWrite}
}

// NotificationHandler handles an inbound JSON-RPC notification.
type NotificationHandler func(ctx context.Context, params json.RawMessage)

// Options configures transport-only behavior. A non-positive frame limit means
// unlimited in that direction. Protocol adapters should always set an inbound
// limit for untrusted peers.
type Options struct {
	MaxInboundBytes  int
	MaxOutboundBytes int
	Name             string
	// StrictJSONRPC validates the jsonrpc member and mutually exclusive frame
	// shapes. Extension Protocol peers always enable it.
	StrictJSONRPC bool
	// MaxConcurrentHandlers bounds inbound request and, unless a notification
	// queue is configured, notification handlers without blocking response
	// dispatch. Non-positive values use the safe default; overload requests
	// receive ErrServerBusy.
	MaxConcurrentHandlers int
	// MaxQueuedNotifications enables ordered notification delivery through one
	// bounded FIFO worker. A full queue fails the connection instead of silently
	// losing a notification. Non-positive values preserve concurrent best-effort
	// notification dispatch for protocols that do not require ordered delivery.
	MaxQueuedNotifications int
	// BeforeRequest runs synchronously on the read loop, after strict frame
	// validation and before a handler goroutine is scheduled. It lets a protocol
	// atomically record wire arrival order (for example, initialize-first) while
	// preserving concurrent handler execution. Returning an error rejects only
	// that request through the normal RPC error mapping.
	BeforeRequest func(method string, params json.RawMessage) error
	// BeforeNotification runs synchronously on the read loop before notification
	// dispatch. Returning an error silently rejects the notification, as required
	// by JSON-RPC, while allowing a protocol to poison transport-local state.
	// The nil default preserves existing protocol behavior.
	BeforeNotification func(method string, params json.RawMessage) error
	// MaxWriteStall bounds how long a single outbound write may make no
	// progress (the peer keeps the pipe open but has stopped reading) before
	// the connection fails with WriteStallError. Non-positive disables the
	// bound, preserving the historical block-forever behavior; stdio peers
	// should always set it, since a wedged child otherwise hangs every caller.
	MaxWriteStall time.Duration
}

// Conn is one bidirectional JSON-RPC 2.0 connection framed as NDJSON.
type Conn struct {
	r    io.Reader
	w    io.Writer
	opts Options

	// Exactly one writer goroutine owns w, fed by the bounded writeQ, so two
	// frames can never interleave on the transport — even when a caller's
	// context aborts mid-flight. writeActive/writeProgress back the optional
	// stall watchdog (MaxWriteStall): a physical write making no progress for
	// the bound fails the connection.
	writeQ        chan writeJob
	writeSlots    chan struct{}
	writeGate     sync.Mutex
	writeClosed   bool
	writerDone    chan struct{}
	writeActive   atomic.Bool
	writeProgress atomic.Int64
	writerOnce    sync.Once

	nextID atomic.Int64

	pmu     sync.Mutex
	pending map[int64]chan rpcResult

	reqH map[string]RequestHandler
	notH map[string]NotificationHandler

	wg             sync.WaitGroup
	closeOnce      sync.Once
	closed         chan struct{}
	closeMu        sync.Mutex
	closeErr       error
	handlerSlots   chan struct{}
	notifyQueue    chan notificationCall
	tryNotifySlots chan struct{}
}

const DefaultMaxConcurrentHandlers = 64

type rpcResult struct {
	result json.RawMessage
	err    error
}

type notificationCall struct {
	handler NotificationHandler
	params  json.RawMessage
}

type outbound struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ErrorObject    `json:"error,omitempty"`
}

type inbound struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *ErrorObject    `json:"error"`
}

// NewConn constructs a connection. Register handlers before calling Serve.
func NewConn(r io.Reader, w io.Writer, opts Options) *Conn {
	if opts.Name == "" {
		opts.Name = "rpcwire"
	}
	if opts.MaxConcurrentHandlers <= 0 {
		opts.MaxConcurrentHandlers = DefaultMaxConcurrentHandlers
	}
	conn := &Conn{
		r:              r,
		w:              w,
		opts:           opts,
		pending:        make(map[int64]chan rpcResult),
		reqH:           make(map[string]RequestHandler),
		notH:           make(map[string]NotificationHandler),
		closed:         make(chan struct{}),
		handlerSlots:   make(chan struct{}, opts.MaxConcurrentHandlers),
		writeQ:         make(chan writeJob, writeQueueLimit),
		writeSlots:     make(chan struct{}, writeQueueLimit),
		writerDone:     make(chan struct{}),
		tryNotifySlots: make(chan struct{}, bestEffortNotifyQueueLimit),
	}
	if opts.MaxQueuedNotifications > 0 {
		conn.notifyQueue = make(chan notificationCall, opts.MaxQueuedNotifications)
	}
	return conn
}

// ensureWriter starts the single writer loop (and the stall watchdog when
// configured) exactly once, lazily on the first write or Serve. Lazy startup
// keeps a Conn that is constructed but never used — an attach rejected before
// Serve, for example — from leaking a permanent goroutine.
func (c *Conn) ensureWriter() {
	c.writerOnce.Do(func() {
		go c.writerLoop()
		if c.opts.MaxWriteStall > 0 {
			go c.stallWatchdog()
		}
	})
}

// Handle registers a request handler. It is not safe to mutate registrations
// concurrently with Serve.
func (c *Conn) Handle(method string, h RequestHandler) { c.reqH[method] = h }

// HandleNotify registers a notification handler.
func (c *Conn) HandleNotify(method string, h NotificationHandler) { c.notH[method] = h }

// Serve reads and dispatches frames until EOF, cancellation observed by the
// read loop, or a framing/read error. In-flight handler contexts are cancelled
// when the transport ends; a product that needs work to outlive the connection
// must derive that work from its own runtime context before returning.
func (c *Conn) Serve(ctx context.Context) error {
	c.ensureWriter()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if c.notifyQueue != nil {
		c.wg.Add(1)
		go c.serveNotifications(ctx)
	}

	br := bufio.NewReaderSize(c.r, 64<<10)
	var loopErr error
	for {
		line, err := readLine(br, c.opts.MaxInboundBytes)
		if len(line) > 0 {
			c.dispatch(ctx, line)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				loopErr = c.decorateReadError(err)
			}
			break
		}
		if err := ctx.Err(); err != nil {
			loopErr = err
			break
		}
	}

	cancel()
	if c.notifyQueue != nil {
		close(c.notifyQueue)
	}
	c.wg.Wait()
	if terminalErr := c.closeReason(); terminalErr != nil {
		loopErr = terminalErr
	}
	c.shutdown(nil)
	return loopErr
}

func (c *Conn) serveNotifications(ctx context.Context) {
	defer c.wg.Done()
	for call := range c.notifyQueue {
		call.handler(ctx, call.params)
	}
}

func (c *Conn) decorateReadError(err error) error {
	var tooLarge *FrameTooLargeError
	if errors.As(err, &tooLarge) {
		return fmt.Errorf("%s: message exceeds size limit: %w", c.opts.Name, err)
	}
	return err
}

func (c *Conn) dispatch(ctx context.Context, line []byte) {
	var in inbound
	if err := json.Unmarshal(line, &in); err != nil {
		if c.opts.StrictJSONRPC && json.Valid(line) {
			c.respondError(json.RawMessage("null"), ErrInvalidRequest, "invalid request", nil)
		} else {
			c.respondError(json.RawMessage("null"), ErrParse, "parse error", nil)
		}
		return
	}
	if c.opts.StrictJSONRPC {
		if err := validateStrictFrame(line, in); err != nil {
			c.respondError(ResponseIDForError(in.ID), ErrInvalidRequest, "invalid request", nil)
			return
		}
	}
	select {
	case <-c.closed:
		return
	default:
	}
	hasID := len(in.ID) > 0
	switch {
	case in.Method != "" && hasID:
		if c.opts.BeforeRequest != nil {
			if err := c.opts.BeforeRequest(in.Method, in.Params); err != nil {
				c.respondHandlerError(in.ID, err)
				return
			}
		}
		if !c.tryStartHandler() {
			c.respondError(in.ID, ErrServerBusy, "server busy", nil)
			return
		}
		c.wg.Add(1)
		go func() {
			defer c.finishHandler()
			defer c.wg.Done()
			c.serveRequest(ctx, in.ID, in.Method, in.Params)
		}()
	case in.Method != "" && !hasID:
		if c.opts.BeforeNotification != nil {
			if err := c.opts.BeforeNotification(in.Method, in.Params); err != nil {
				return
			}
		}
		if h := c.notH[in.Method]; h != nil {
			if c.notifyQueue != nil {
				select {
				case c.notifyQueue <- notificationCall{handler: h, params: in.Params}:
				default:
					c.fail(fmt.Errorf("%s: notification queue overflow", c.opts.Name))
				}
				return
			}
			if !c.tryStartHandler() {
				return
			}
			c.wg.Add(1)
			go func() {
				defer c.finishHandler()
				defer c.wg.Done()
				h(ctx, in.Params)
			}()
		}
	case in.Method == "" && hasID:
		c.resolve(in)
	default:
		c.respondError(json.RawMessage("null"), ErrInvalidRequest, "invalid request", nil)
	}
}

func (c *Conn) tryStartHandler() bool {
	select {
	case c.handlerSlots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (c *Conn) finishHandler() { <-c.handlerSlots }

func validateStrictFrame(line []byte, in inbound) error {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(line, &members); err != nil {
		return err
	}
	if in.JSONRPC != "2.0" {
		return errors.New("jsonrpc must be 2.0")
	}
	_, hasID := members["id"]
	_, hasMethod := members["method"]
	_, hasParams := members["params"]
	_, hasResult := members["result"]
	_, hasError := members["error"]
	if hasID && !validRPCID(in.ID) {
		return errors.New("id must be a string, integer, or null")
	}
	if hasMethod {
		if in.Method == "" || hasResult || hasError {
			return errors.New("invalid request shape")
		}
		if hasParams {
			trimmed := bytes.TrimSpace(in.Params)
			if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
				return errors.New("params must be object or array")
			}
		}
		return nil
	}
	if !hasID || hasParams || hasResult == hasError {
		return errors.New("invalid response shape")
	}
	if hasError && in.Error == nil {
		return errors.New("invalid error object")
	}
	if hasError {
		var errorMembers map[string]json.RawMessage
		if err := json.Unmarshal(members["error"], &errorMembers); err != nil {
			return errors.New("invalid error object")
		}
		if _, ok := errorMembers["code"]; !ok {
			return errors.New("error code is required")
		}
		if _, ok := errorMembers["message"]; !ok {
			return errors.New("error message is required")
		}
	}
	return nil
}

func validRPCID(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if bytes.Equal(raw, []byte("null")) {
		return true
	}
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		return true
	}
	if len(raw) == 0 {
		return false
	}
	i := 0
	if raw[0] == '-' {
		i++
		if i == len(raw) {
			return false
		}
	}
	if raw[i] == '0' && i+1 != len(raw) {
		return false
	}
	for ; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return false
		}
	}
	return true
}

func (c *Conn) serveRequest(ctx context.Context, id json.RawMessage, method string, params json.RawMessage) {
	h := c.reqH[method]
	if h == nil {
		c.respondError(id, ErrMethodNotFound, "method not found: "+method, nil)
		return
	}
	result, err := h(ctx, params)
	if err != nil {
		c.respondHandlerError(id, err)
		return
	}
	var afterWrite func(error)
	if response, ok := result.(HandlerResponse); ok {
		result = response.Result
		afterWrite = response.AfterWrite
	}
	raw, err := json.Marshal(result)
	if err != nil {
		c.respondError(id, ErrInternal, "marshal result: "+err.Error(), nil)
		c.runAfterWrite(afterWrite, err)
		return
	}
	writeErr := c.write(context.Background(), outbound{JSONRPC: "2.0", ID: id, Result: raw})
	if writeErr != nil {
		var tooLarge *FrameTooLargeError
		if errors.As(writeErr, &tooLarge) {
			c.respondError(id, ErrInternal, "response exceeds frame size limit", nil)
			c.runAfterWrite(afterWrite, writeErr)
			return
		}
		c.fail(writeErr)
	}
	c.runAfterWrite(afterWrite, writeErr)
}

func (c *Conn) runAfterWrite(callback func(error), writeErr error) {
	if callback == nil {
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			c.fail(fmt.Errorf("%s: after-response callback panic: %v", c.opts.Name, recovered))
		}
	}()
	callback(writeErr)
}

func (c *Conn) respondHandlerError(id json.RawMessage, err error) {
	code := ErrInternal
	message := err.Error()
	var data any
	var re *RPCError
	if errors.As(err, &re) {
		code = re.Code
		message = re.Message
		data = re.Data
	}
	c.respondError(id, code, message, data)
}

func (c *Conn) resolve(in inbound) {
	id, err := strconv.ParseInt(string(in.ID), 10, 64)
	if err != nil {
		return
	}
	c.pmu.Lock()
	ch := c.pending[id]
	delete(c.pending, id)
	c.pmu.Unlock()
	if ch == nil {
		return
	}
	if in.Error != nil {
		ch <- rpcResult{err: &ResponseError{Code: in.Error.Code, Message: in.Error.Message, Data: in.Error.Data}}
		return
	}
	ch <- rpcResult{result: in.Result}
}

// Notify sends a fire-and-forget notification.
func (c *Conn) Notify(method string, params any) error {
	m, err := notification(method, params)
	if err != nil {
		return err
	}
	err = c.write(context.Background(), m)
	if err != nil {
		var tooLarge *FrameTooLargeError
		if !errors.As(err, &tooLarge) {
			c.fail(err)
		}
	}
	return err
}

// TryNotify enqueues a fire-and-forget notification without waiting for a
// physical write. A nil result means the bounded writer accepted the frame,
// not that the peer has processed it. When the queue is full it returns
// OutboundQueueFullError immediately, allowing observation-only callers to
// drop the event instead of adding sidecar backpressure to a host hot path.
func (c *Conn) TryNotify(method string, params any) error {
	m, err := notification(method, params)
	if err != nil {
		return err
	}
	job, err := c.prepareWrite(m, context.Background())
	if err != nil {
		return err
	}
	select {
	case c.tryNotifySlots <- struct{}{}:
		job.release = func() { <-c.tryNotifySlots }
	default:
		return &OutboundQueueFullError{Limit: cap(c.tryNotifySlots)}
	}
	if err := c.enqueueWrite(job, false); err != nil {
		job.release()
		return err
	}
	return nil
}

func notification(method string, params any) (outbound, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return outbound{}, err
	}
	return outbound{JSONRPC: "2.0", Method: method, Params: raw}, nil
}

// Request sends a request and waits for its response, cancellation, or closure.
func (c *Conn) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	id := c.nextID.Add(1)
	ch := make(chan rpcResult, 1)
	c.pmu.Lock()
	select {
	case <-c.closed:
		c.pmu.Unlock()
		closedErr := c.terminalError()
		if closedErr == nil {
			closedErr = fmt.Errorf("%s: connection closed", c.opts.Name)
		}
		return nil, closedErr
	default:
	}
	c.pending[id] = ch
	c.pmu.Unlock()
	defer func() {
		c.pmu.Lock()
		delete(c.pending, id)
		c.pmu.Unlock()
	}()

	idRaw, _ := json.Marshal(id)
	if err := c.write(ctx, outbound{JSONRPC: "2.0", ID: idRaw, Method: method, Params: raw}); err != nil {
		var tooLarge *FrameTooLargeError
		// A caller-context abort (turn cancel, per-call timeout) fails only
		// this request — the connection stays usable. Genuine transport
		// failures, including a write that stalled past MaxWriteStall, fail
		// the connection so a wedged peer is torn down.
		if !errors.As(err, &tooLarge) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			c.fail(err)
		}
		return nil, err
	}
	select {
	case res := <-ch:
		return res.result, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *Conn) write(ctx context.Context, m outbound) error {
	job, err := c.prepareWrite(m, ctx)
	if err != nil {
		return err
	}
	if err := c.enqueueWrite(job, true); err != nil {
		return err
	}
	select {
	case err := <-job.res:
		return err
	case <-job.ctx.Done():
		// The caller gave up: the writer loop will skip the frame if it has
		// not physically started, or finish it serially if it has — the
		// transport never sees a torn or interleaved frame.
		return job.ctx.Err()
	case <-c.closed:
		// A completed write may race the connection's teardown; the buffered
		// result is already there when that happened, so prefer it over the
		// terminal error (the response-close regression class).
		select {
		case err := <-job.res:
			return err
		default:
		}
		return c.terminalError()
	}
}

// enqueueWrite reserves bounded queue capacity before entering writeGate.
// The reservation makes the send non-blocking while the gate is held, so
// shutdown can close writeQ without racing a producer or waiting behind a
// producer blocked on a full queue. blocking is false for TryNotify.
func (c *Conn) enqueueWrite(job writeJob, blocking bool) error {
	c.ensureWriter()
	if blocking {
		select {
		case c.writeSlots <- struct{}{}:
		case <-job.ctx.Done():
			return job.ctx.Err()
		case <-c.closed:
			return c.terminalError()
		}
	} else {
		select {
		case c.writeSlots <- struct{}{}:
		default:
			return &OutboundQueueFullError{Limit: cap(c.tryNotifySlots)}
		}
	}

	c.writeGate.Lock()
	defer c.writeGate.Unlock()
	if c.writeClosed {
		<-c.writeSlots
		return c.terminalError()
	}
	// A reserved slot guarantees capacity; the send cannot block while the
	// gate is held. Keeping the normal send makes accounting bugs fail loudly.
	c.writeQ <- job
	return nil
}

func (c *Conn) prepareWrite(m outbound, ctx context.Context) (writeJob, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return writeJob{}, err
	}
	if limit := c.opts.MaxOutboundBytes; limit > 0 && buf.Len() > limit {
		return writeJob{}, &FrameTooLargeError{Direction: "outbound", Size: buf.Len(), Limit: limit}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return writeJob{frame: buf.Bytes(), ctx: ctx, res: make(chan error, 1)}, nil
}

// writeQueueLimit bounds queued outbound frames per connection. A wedged
// peer fills the queue and then the stall watchdog fails the connection;
// senders never block unboundedly behind it.
const writeQueueLimit = 256

// bestEffortNotifyQueueLimit prevents observation events from filling the
// shared writer queue ahead of request/response traffic. Sixteen queued or
// in-flight events absorb healthy bursts while preserving capacity and
// latency for blocking intercept, provider, UI, and shutdown calls.
const bestEffortNotifyQueueLimit = 16

// writeJob is one outbound frame awaiting the single writer goroutine.
type writeJob struct {
	frame   []byte
	ctx     context.Context // pre-write cancellation only
	res     chan error      // buffered 1
	release func()          // releases optional best-effort notification capacity
}

func completeWriteJob(job writeJob, err error) {
	job.res <- err
	if job.release != nil {
		job.release()
	}
}

// writerLoop is the ONLY writer of c.w. It drains the queue in order, skips
// frames whose caller already gave up before the physical write began, and
// finishes any frame it started — frames are atomic and ordered by
// construction. On connection close it fails everything still queued.
func (c *Conn) writerLoop() {
	defer close(c.writerDone)
	for job := range c.writeQ {
		<-c.writeSlots
		c.writeGate.Lock()
		closed := c.writeClosed
		c.writeGate.Unlock()
		if closed {
			completeWriteJob(job, c.terminalError())
			continue
		}
		if job.ctx != nil {
			if err := job.ctx.Err(); err != nil {
				completeWriteJob(job, err)
				continue
			}
		}
		err := c.writeAll(job.frame)
		if err != nil {
			// When the connection is already terminal, report the root
			// cause (e.g. the stall watchdog's WriteStallError) rather
			// than its side effect — a transport closing underneath an
			// in-flight write surfaces as a plain closed-pipe error.
			select {
			case <-c.closed:
				if terminal := c.terminalError(); terminal != nil {
					err = terminal
				}
			default:
			}
		}
		completeWriteJob(job, err)
		if err != nil {
			// shutdown waits for writerDone. Run it outside this goroutine so
			// writerLoop can return and satisfy that lifecycle handshake.
			go c.fail(err)
			return
		}
	}
}

// writeAll serially writes one frame, marking activity for the stall
// watchdog before every blocking Write. It only returns on completion or a
// transport error — caller cancellation never tears a frame in half.
func (c *Conn) writeAll(b []byte) error {
	for len(b) > 0 {
		c.writeProgress.Store(time.Now().UnixNano())
		c.writeActive.Store(true)
		n, err := c.w.Write(b)
		c.writeActive.Store(false)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}

// stallWatchdog fails the connection when a physical write makes no progress
// for MaxWriteStall: the peer is alive enough to hold the pipe open but has
// stopped reading, and without a bound every later frame would queue behind
// it forever. The watchdog is deliberately independent of any caller
// context, so a short per-call timeout cannot preempt it.
func (c *Conn) stallWatchdog() {
	interval := c.opts.MaxWriteStall / 2
	if interval <= 0 {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if !c.writeActive.Load() {
				continue
			}
			last := time.Unix(0, c.writeProgress.Load())
			if time.Since(last) > c.opts.MaxWriteStall {
				c.fail(&WriteStallError{Direction: "outbound", Stall: c.opts.MaxWriteStall})
				return
			}
		case <-c.closed:
			return
		}
	}
}

func (c *Conn) writeError(id json.RawMessage, code int, message string, data any) error {
	var raw json.RawMessage
	if data != nil {
		encoded, err := json.Marshal(data)
		if err != nil {
			code = ErrInternal
			message = "marshal error data: " + err.Error()
		} else if string(encoded) != "null" {
			raw = encoded
		}
	}
	return c.write(context.Background(), outbound{JSONRPC: "2.0", ID: id, Error: &ErrorObject{Code: code, Message: message, Data: raw}})
}

func (c *Conn) respondError(id json.RawMessage, code int, message string, data any) {
	err := c.writeError(id, code, message, data)
	var tooLarge *FrameTooLargeError
	if errors.As(err, &tooLarge) && (data != nil || code != ErrInternal || message != "error response exceeds frame size limit") {
		err = c.writeError(id, ErrInternal, "error response exceeds frame size limit", nil)
	}
	if err != nil {
		c.fail(err)
	}
}

func (c *Conn) fail(err error) {
	if err == nil {
		return
	}
	c.shutdown(err)
	if closer, ok := c.r.(io.Closer); ok {
		_ = closer.Close()
	}
}

// closeReason returns the stored terminal error as-is (nil on a clean EOF);
// Serve uses it so a graceful end still reports nil.
func (c *Conn) closeReason() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	return c.closeErr
}

// terminalError is the error every caller observes after the connection
// ends. It is never nil — a graceful EOF must not report silently dropped
// writes as successes.
func (c *Conn) terminalError() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closeErr != nil {
		return c.closeErr
	}
	return fmt.Errorf("%s: connection closed", c.opts.Name)
}

// writerExitWaitBound caps how long shutdown lets an in-flight physical write
// finish. A wedged writer is failed by the stall watchdog; teardown itself must
// still be bounded.
const writerExitWaitBound = 100 * time.Millisecond

func (c *Conn) shutdown(err error) {
	c.closeOnce.Do(func() {
		c.closeMu.Lock()
		c.closeErr = err
		c.closeMu.Unlock()

		// Linearize closure against every producer, then close the queue. No
		// producer can send after writeClosed becomes visible because enqueue
		// performs its final check and send under the same gate.
		c.ensureWriter()
		c.writeGate.Lock()
		c.writeClosed = true
		close(c.writeQ)
		c.writeGate.Unlock()
		select {
		case <-c.writerDone:
		case <-time.After(writerExitWaitBound):
		}
		close(c.closed)
		c.pmu.Lock()
		for id, ch := range c.pending {
			closedErr := err
			if closedErr == nil {
				closedErr = fmt.Errorf("%s: connection closed", c.opts.Name)
			}
			ch <- rpcResult{err: closedErr}
			delete(c.pending, id)
		}
		c.pmu.Unlock()
	})
}
