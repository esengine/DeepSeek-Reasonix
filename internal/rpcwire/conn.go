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
	// shapes. ACP leaves this off for compatibility; Remote enables it.
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
}

// Conn is one bidirectional JSON-RPC 2.0 connection framed as NDJSON.
type Conn struct {
	r    io.Reader
	w    io.Writer
	opts Options

	wmu sync.Mutex

	nextID atomic.Int64

	pmu     sync.Mutex
	pending map[int64]chan rpcResult

	reqH map[string]RequestHandler
	notH map[string]NotificationHandler

	wg           sync.WaitGroup
	closeOnce    sync.Once
	closed       chan struct{}
	closeMu      sync.Mutex
	closeErr     error
	handlerSlots chan struct{}
	notifyQueue  chan notificationCall
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
		r:            r,
		w:            w,
		opts:         opts,
		pending:      make(map[int64]chan rpcResult),
		reqH:         make(map[string]RequestHandler),
		notH:         make(map[string]NotificationHandler),
		closed:       make(chan struct{}),
		handlerSlots: make(chan struct{}, opts.MaxConcurrentHandlers),
	}
	if opts.MaxQueuedNotifications > 0 {
		conn.notifyQueue = make(chan notificationCall, opts.MaxQueuedNotifications)
	}
	return conn
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
	if terminalErr := c.terminalError(); terminalErr != nil {
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
	writeErr := c.write(outbound{JSONRPC: "2.0", ID: id, Result: raw})
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
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	err = c.write(outbound{JSONRPC: "2.0", Method: method, Params: raw})
	if err != nil {
		var tooLarge *FrameTooLargeError
		if !errors.As(err, &tooLarge) {
			c.fail(err)
		}
	}
	return err
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
	if err := c.write(outbound{JSONRPC: "2.0", ID: idRaw, Method: method, Params: raw}); err != nil {
		var tooLarge *FrameTooLargeError
		if !errors.As(err, &tooLarge) {
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

func (c *Conn) write(m outbound) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return err
	}
	if limit := c.opts.MaxOutboundBytes; limit > 0 && buf.Len() > limit {
		return &FrameTooLargeError{Direction: "outbound", Size: buf.Len(), Limit: limit}
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	for buf.Len() > 0 {
		n, err := c.w.Write(buf.Bytes())
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		buf.Next(n)
	}
	return nil
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
	return c.write(outbound{JSONRPC: "2.0", ID: id, Error: &ErrorObject{Code: code, Message: message, Data: raw}})
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

func (c *Conn) terminalError() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	return c.closeErr
}

func (c *Conn) shutdown(err error) {
	c.closeOnce.Do(func() {
		c.closeMu.Lock()
		c.closeErr = err
		c.closeMu.Unlock()
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
