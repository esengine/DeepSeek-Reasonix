package extension

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
	"sync"
	"sync/atomic"
)

// Standard JSON-RPC 2.0 error codes, plus the extension domain code and the
// transport-local overload code.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternal       = -32603
	// CodeServerBusy is a stable transport-local overload response. It is
	// outside the JSON-RPC reserved range and intentionally carries no peer
	// data.
	CodeServerBusy = -32099
)

// Transport bounds.
const (
	// maxConcurrentHandlers bounds inbound request and notification handlers.
	maxConcurrentHandlers = 32
	// maxQueuedNotifications bounds the outbound notification queue. A full
	// queue fails the connection rather than silently dropping a provider
	// stream chunk (a dropped chunk would surface as a stream_gap host-side);
	// this mirrors the host side's policy.
	maxQueuedNotifications = 256
)

// ResponseError is returned by outbound calls when the peer answers with a
// JSON-RPC error. Data remains raw so callers can decode ProtocolErrorData.
type ResponseError struct {
	Code    int
	Message string
	Data    json.RawMessage
}

func (e *ResponseError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// FrameTooLargeError reports a frame that violates the frozen NDJSON budget.
// Size and Limit include the trailing newline, matching the bytes sent over
// the transport.
type FrameTooLargeError struct {
	Direction string
	Size      int
	Limit     int
}

func (e *FrameTooLargeError) Error() string {
	return fmt.Sprintf("extension: %s frame is %d bytes; limit is %d", e.Direction, e.Size, e.Limit)
}

// rpcErrorObject is the JSON-RPC error object carried on the wire.
type rpcErrorObject struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// requestHandler answers an inbound JSON-RPC request.
type requestHandler func(ctx context.Context, params json.RawMessage) (any, error)

// notificationHandler handles an inbound JSON-RPC notification.
type notificationHandler func(ctx context.Context, params json.RawMessage)

// deferredResult lets a request handler run cleanup only after a successful
// response write (for example, starting a provider stream pump once the
// stream/open acknowledgment is on the wire).
type deferredResult struct {
	result any
	after  func()
}

type rpcResult struct {
	result json.RawMessage
	err    error
}

type outbound struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcErrorObject `json:"error,omitempty"`
}

type inbound struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcErrorObject `json:"error"`
}

// conn is one bidirectional strict JSON-RPC 2.0 connection framed as NDJSON.
// The extension dialect narrows generic JSON-RPC: ids are integers only and
// params must be JSON objects.
type conn struct {
	r   io.Reader
	w   io.Writer
	log *log.Logger

	wmu sync.Mutex

	nextID atomic.Int64

	pmu     sync.Mutex
	pending map[int64]chan rpcResult

	reqH map[string]requestHandler
	notH map[string]notificationHandler

	// beforeRequest and beforeNotification run synchronously on the read loop
	// after strict frame validation and before dispatch, letting the
	// handshake barrier observe wire arrival order.
	beforeRequest      func(method string) error
	beforeNotification func(method string) error

	wg           sync.WaitGroup
	closeOnce    sync.Once
	closed       chan struct{}
	closeMu      sync.Mutex
	closeErr     error
	handlerSlots chan struct{}
	notifyQueue  chan []byte
}

func newConn(r io.Reader, w io.Writer, logger *log.Logger) *conn {
	return &conn{
		r:            r,
		w:            w,
		log:          logger,
		pending:      make(map[int64]chan rpcResult),
		reqH:         make(map[string]requestHandler),
		notH:         make(map[string]notificationHandler),
		closed:       make(chan struct{}),
		handlerSlots: make(chan struct{}, maxConcurrentHandlers),
		notifyQueue:  make(chan []byte, maxQueuedNotifications),
	}
}

// serve reads and dispatches frames until EOF, cancellation, or a
// framing/read error. In-flight handler contexts are cancelled when the
// transport ends.
func (c *conn) serve(ctx context.Context) error {
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	c.wg.Add(1)
	go c.serveOutboundNotifications()

	// Unblock a read parked on ctx cancellation: closing the reader is the
	// only reliable way to interrupt it.
	if closer, ok := c.r.(io.Closer); ok {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			select {
			case <-serveCtx.Done():
				_ = closer.Close()
			case <-c.closed:
			}
		}()
	}

	br := bufio.NewReaderSize(c.r, 64<<10)
	var loopErr error
	for {
		line, err := readLine(br, FrameBytes)
		if len(line) > 0 {
			c.dispatch(serveCtx, line)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				loopErr = err
			}
			break
		}
		if err := serveCtx.Err(); err != nil {
			loopErr = err
			break
		}
	}

	cancel()
	close(c.notifyQueue)
	c.wg.Wait()
	// A connection that was failed or shut down deliberately makes the
	// resulting read error a consequence, not the cause: report the recorded
	// terminal error (nil for an orderly shutdown). Otherwise a parent ctx
	// cancellation explains the forced reader close.
	select {
	case <-c.closed:
		loopErr = c.recordedCloseError()
	default:
		if err := ctx.Err(); err != nil {
			loopErr = err
		}
	}
	c.shutdown(loopErr)
	return loopErr
}

// serveOutboundNotifications is the single ordered writer for fire-and-forget
// notifications (provider stream chunks). The queue is bounded; a full queue
// fails the connection instead of dropping a frame.
func (c *conn) serveOutboundNotifications() {
	defer c.wg.Done()
	for frame := range c.notifyQueue {
		if err := c.writeFrame(frame); err != nil {
			c.fail(err)
			return
		}
	}
}

func (c *conn) dispatch(ctx context.Context, line []byte) {
	var in inbound
	if err := json.Unmarshal(line, &in); err != nil {
		if json.Valid(line) {
			c.respondError(json.RawMessage("null"), CodeInvalidRequest, "invalid request", nil)
		} else {
			c.respondError(json.RawMessage("null"), CodeParseError, "parse error", nil)
		}
		return
	}
	if err := validateStrictFrame(line, &in); err != nil {
		if c.log != nil {
			c.log.Printf("extension: rejecting frame: %v", err)
		}
		c.respondError(responseIDForError(in.ID), CodeInvalidRequest, "invalid request", nil)
		return
	}
	select {
	case <-c.closed:
		return
	default:
	}
	hasID := len(in.ID) > 0
	switch {
	case in.Method != "" && hasID:
		if c.beforeRequest != nil {
			if err := c.beforeRequest(in.Method); err != nil {
				c.respondHandlerError(in.ID, err)
				return
			}
		}
		if !c.tryStartHandler() {
			c.respondError(in.ID, CodeServerBusy, "server busy", nil)
			return
		}
		c.wg.Add(1)
		go func() {
			defer c.finishHandler()
			defer c.wg.Done()
			c.serveRequest(ctx, in.ID, in.Method, in.Params)
		}()
	case in.Method != "" && !hasID:
		if c.beforeNotification != nil {
			if err := c.beforeNotification(in.Method); err != nil {
				return
			}
		}
		h := c.notH[in.Method]
		if h == nil {
			if c.log != nil {
				c.log.Printf("extension: dropping notification for unhandled method %q", in.Method)
			}
			return
		}
		// No response is possible for a notification, so a saturated handler
		// pool drops with a diagnostic rather than failing the connection.
		if !c.tryStartHandler() {
			if c.log != nil {
				c.log.Printf("extension: dropping %q notification: handler pool saturated", in.Method)
			}
			return
		}
		c.wg.Add(1)
		go func() {
			defer c.finishHandler()
			defer c.wg.Done()
			c.runNotification(ctx, h, in.Params)
		}()
	case in.Method == "" && hasID:
		c.resolve(&in)
	default:
		c.respondError(json.RawMessage("null"), CodeInvalidRequest, "invalid request", nil)
	}
}

func (c *conn) tryStartHandler() bool {
	select {
	case c.handlerSlots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (c *conn) finishHandler() { <-c.handlerSlots }

// validateStrictFrame enforces the extension dialect of JSON-RPC 2.0:
// jsonrpc=="2.0", request/response shapes are mutually exclusive, ids are
// integers (or null), and params, when present, is a JSON object.
func validateStrictFrame(line []byte, in *inbound) error {
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
		return errors.New("id must be an integer or null")
	}
	if hasMethod {
		if in.Method == "" || hasResult || hasError {
			return errors.New("invalid request shape")
		}
		if hasParams {
			trimmed := bytes.TrimSpace(in.Params)
			if len(trimmed) == 0 || trimmed[0] != '{' {
				return errors.New("params must be a JSON object")
			}
		}
		return nil
	}
	if !hasID || hasParams || hasResult == hasError {
		return errors.New("invalid response shape")
	}
	if hasError {
		if in.Error == nil {
			return errors.New("invalid error object")
		}
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

// validRPCID reports whether raw is an integer or null id. Unlike generic
// JSON-RPC, the extension protocol does not use string ids.
func validRPCID(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if bytes.Equal(raw, []byte("null")) {
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

// responseIDForError extracts the id member for an error response to a
// rejected frame, falling back to null when the id is absent or invalid.
func responseIDForError(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 || !validRPCID(raw) {
		return json.RawMessage("null")
	}
	return raw
}

func (c *conn) serveRequest(ctx context.Context, id json.RawMessage, method string, params json.RawMessage) {
	h := c.reqH[method]
	if h == nil {
		notFound := MustProtocolError(ErrUnknownMethod)
		spec := frozenErrorSpecs[ErrUnknownMethod]
		c.respondError(id, spec.Code, "method not found: "+method, ProtocolErrorData{Reason: notFound.Reason, Retryable: spec.Retryable})
		return
	}
	result, err := c.runHandler(ctx, h, params)
	if err != nil {
		c.respondHandlerError(id, err)
		return
	}
	var after func()
	if deferred, ok := result.(deferredResult); ok {
		result = deferred.result
		after = deferred.after
	}
	raw, err := json.Marshal(result)
	if err != nil {
		c.respondError(id, CodeInternal, "marshal result: "+err.Error(), nil)
		return
	}
	writeErr := c.write(outbound{JSONRPC: "2.0", ID: id, Result: raw})
	if writeErr != nil {
		var tooLarge *FrameTooLargeError
		if errors.As(writeErr, &tooLarge) {
			c.respondError(id, CodeInternal, "response exceeds frame size limit", nil)
			return
		}
		c.fail(writeErr)
		return
	}
	if after != nil {
		c.runAfterWrite(after)
	}
}

// runNotification executes one notification handler, converting a panic into
// a diagnostic so the read loop and the connection survive.
func (c *conn) runNotification(ctx context.Context, h notificationHandler, params json.RawMessage) {
	defer func() {
		if recovered := recover(); recovered != nil && c.log != nil {
			c.log.Printf("extension: notification handler panic: %v", recovered)
		}
	}()
	h(ctx, params)
}

// runHandler executes one request handler, converting a panic into the frozen
// internal error so the read loop and the connection survive.
func (c *conn) runHandler(ctx context.Context, h requestHandler, params json.RawMessage) (result any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if c.log != nil {
				c.log.Printf("extension: handler panic: %v", recovered)
			}
			result = nil
			err = MustProtocolError(ErrInternal)
		}
	}()
	return h(ctx, params)
}

func (c *conn) runAfterWrite(after func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			c.fail(fmt.Errorf("extension: after-response callback panic: %v", recovered))
		}
	}()
	after()
}

func (c *conn) respondHandlerError(id json.RawMessage, err error) {
	// A fatal error (a failed handshake) is answered first and only then ends
	// the connection, so the peer sees the reason.
	var fatal *fatalError
	isFatal := errors.As(err, &fatal)
	respond := err
	if isFatal {
		respond = fatal.err
	}
	var protocolErr *ProtocolError
	if errors.As(respond, &protocolErr) {
		spec := frozenErrorSpecs[protocolErr.Reason]
		message := protocolErr.Message
		if message == "" {
			message = spec.Message
		}
		c.respondError(id, spec.Code, message, ProtocolErrorData{Reason: protocolErr.Reason, Retryable: spec.Retryable})
	} else {
		if c.log != nil {
			c.log.Printf("extension: handler error: %v", respond)
		}
		// Unknown handler errors never leak internals onto the wire: the peer
		// sees the frozen internal error, the diagnostic goes to the logger.
		spec := frozenErrorSpecs[ErrInternal]
		c.respondError(id, spec.Code, spec.Message, ProtocolErrorData{Reason: ErrInternal, Retryable: spec.Retryable})
	}
	if isFatal {
		c.fail(fatal.err)
	}
}

func (c *conn) resolve(in *inbound) {
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

// notify queues a fire-and-forget notification. Notifications travel through
// one bounded FIFO queue so provider stream chunks stay ordered; a full queue
// fails the connection rather than silently dropping a frame (mirroring the
// host side). A marshaled frame beyond FrameBytes fails only that call.
func (c *conn) notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(outbound{JSONRPC: "2.0", Method: method, Params: raw}); err != nil {
		return err
	}
	if buf.Len() > FrameBytes {
		return &FrameTooLargeError{Direction: "outbound", Size: buf.Len(), Limit: FrameBytes}
	}
	select {
	case <-c.closed:
		return c.closedError()
	default:
	}
	select {
	case c.notifyQueue <- buf.Bytes():
		return nil
	default:
		err := fmt.Errorf("extension: outbound notification queue overflow (%d)", maxQueuedNotifications)
		c.fail(err)
		return err
	}
}

// call sends a request and waits for its response, cancellation, or closure.
func (c *conn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
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
		return nil, c.closedError()
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
	case <-c.closed:
		return nil, c.closedError()
	}
}

func (c *conn) write(m outbound) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return err
	}
	if buf.Len() > FrameBytes {
		return &FrameTooLargeError{Direction: "outbound", Size: buf.Len(), Limit: FrameBytes}
	}
	return c.writeFrame(buf.Bytes())
}

func (c *conn) writeFrame(frame []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	for len(frame) > 0 {
		n, err := c.w.Write(frame)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		frame = frame[n:]
	}
	return nil
}

func (c *conn) respondError(id json.RawMessage, code int, message string, data any) {
	var raw json.RawMessage
	if data != nil {
		encoded, err := json.Marshal(data)
		if err != nil {
			code = CodeInternal
			message = "marshal error data: " + err.Error()
		} else if string(encoded) != "null" {
			raw = encoded
		}
	}
	if err := c.write(outbound{JSONRPC: "2.0", ID: id, Error: &rpcErrorObject{Code: code, Message: message, Data: raw}}); err != nil {
		var tooLarge *FrameTooLargeError
		if !errors.As(err, &tooLarge) {
			c.fail(err)
		}
	}
}

func (c *conn) fail(err error) {
	if err == nil {
		return
	}
	c.shutdown(err)
	if closer, ok := c.r.(io.Closer); ok {
		_ = closer.Close()
	}
}

func (c *conn) closedError() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closeErr != nil {
		return c.closeErr
	}
	return errors.New("extension: connection closed")
}

// recordedCloseError returns the terminal error recorded at shutdown, which
// may be nil for an orderly close.
func (c *conn) recordedCloseError() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	return c.closeErr
}

func (c *conn) shutdown(err error) {
	c.closeOnce.Do(func() {
		c.closeMu.Lock()
		c.closeErr = err
		c.closeMu.Unlock()
		close(c.closed)
		c.pmu.Lock()
		for id, ch := range c.pending {
			pendingErr := err
			if pendingErr == nil {
				pendingErr = errors.New("extension: connection closed")
			}
			ch <- rpcResult{err: pendingErr}
			delete(c.pending, id)
		}
		c.pmu.Unlock()
	})
}

// readLine reads one NDJSON frame, enforcing the byte budget across bufio
// refills and trimming the trailing line ending.
func readLine(br *bufio.Reader, maxBytes int) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := br.ReadSlice('\n')
		buf = append(buf, chunk...)
		if maxBytes > 0 && len(buf) > maxBytes {
			return nil, &FrameTooLargeError{Direction: "inbound", Size: len(buf), Limit: maxBytes}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		n := len(buf)
		for n > 0 && (buf[n-1] == '\n' || buf[n-1] == '\r') {
			n--
		}
		return trimSpaceBytes(buf[:n]), err
	}
}

func trimSpaceBytes(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && isSpaceByte(b[i]) {
		i++
	}
	for j > i && isSpaceByte(b[j-1]) {
		j--
	}
	return b[i:j]
}

func isSpaceByte(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
