package extension

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"sync"
	"testing"
	"time"
)

// fakeHost is a scriptable in-memory Reasonix host speaking raw JSON-RPC
// over two io.Pipes: it writes Host → Extension frames into the SDK's stdin
// pipe and reads the SDK's stdout pipe, answering Extension → Host requests
// with scripted handlers.
type fakeHost struct {
	t *testing.T

	toSDK   *io.PipeWriter // host writes, SDK reads
	fromSDK *io.PipeReader // SDK writes, host reads

	writeMu sync.Mutex
	nextID  int64

	pendingMu sync.Mutex
	pending   map[int64]chan hostResponse

	strayMu    sync.Mutex
	strays     []strayResponse
	handlersMu sync.Mutex
	handlers   map[string]func(params json.RawMessage) (any, *hostError)
	requestLog map[string][]json.RawMessage

	notesMu       sync.Mutex
	notifications []hostNotification

	readerDone chan struct{}
}

type hostError struct {
	Code    int
	Message string
	Data    any
}

type hostResponse struct {
	Result json.RawMessage
	Err    *hostError
}

// strayResponse is an SDK response with no matching pending host request —
// typically a -32600 rejection of a malformed raw frame.
type strayResponse struct {
	ID    json.RawMessage
	Error *hostError
}

type hostNotification struct {
	Method string
	Params json.RawMessage
}

type hostFrame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *hostErrorFrame `json:"error"`
}

type hostErrorFrame struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// serveWaiter caches Serve's result so both the test and its cleanup can
// observe it exactly once.
type serveWaiter struct {
	ch       chan error
	mu       sync.Mutex
	err      error
	received bool
}

// wait blocks up to timeout for Serve's result; later calls return the
// cached value. ok is false on timeout.
func (w *serveWaiter) wait(timeout time.Duration) (err error, ok bool) {
	w.mu.Lock()
	if w.received {
		w.mu.Unlock()
		return w.err, true
	}
	w.mu.Unlock()
	select {
	case err := <-w.ch:
		w.mu.Lock()
		w.err = err
		w.received = true
		w.mu.Unlock()
		return err, true
	case <-time.After(timeout):
		return nil, false
	}
}

// startFakeHost launches Serve against a fake host and returns both. The
// host's read loop answers SDK requests until cleanup.
func startFakeHost(t *testing.T, h Handler, opts Options) (*fakeHost, *serveWaiter) {
	t.Helper()
	sdkStdinR, sdkStdinW := io.Pipe()
	sdkStdoutR, sdkStdoutW := io.Pipe()
	opts.Stdin = sdkStdinR
	opts.Stdout = sdkStdoutW
	if opts.Logger == nil {
		opts.Logger = log.New(io.Discard, "", 0)
	}
	waiter := &serveWaiter{ch: make(chan error, 1)}
	go func() { waiter.ch <- Serve(context.Background(), h, opts) }()
	host := &fakeHost{
		t:          t,
		toSDK:      sdkStdinW,
		fromSDK:    sdkStdoutR,
		pending:    make(map[int64]chan hostResponse),
		handlers:   make(map[string]func(json.RawMessage) (any, *hostError)),
		requestLog: make(map[string][]json.RawMessage),
		readerDone: make(chan struct{}),
	}
	go host.readLoop()
	t.Cleanup(func() {
		_ = host.toSDK.Close()
		_ = host.fromSDK.Close()
		<-host.readerDone
		if _, ok := waiter.wait(5 * time.Second); !ok {
			t.Errorf("Serve did not return after the transport closed")
		}
	})
	return host, waiter
}

// readLoop consumes every frame the SDK writes: responses resolve pending
// host requests, requests are routed to scripted handlers, notifications are
// recorded.
func (h *fakeHost) readLoop() {
	defer close(h.readerDone)
	scanner := bufio.NewScanner(h.fromSDK)
	scanner.Buffer(make([]byte, 0, 64<<10), FrameBytes*2)
	for scanner.Scan() {
		line := scanner.Bytes()
		var frame hostFrame
		if err := json.Unmarshal(line, &frame); err != nil {
			h.t.Errorf("fake host: undecodable SDK frame %q: %v", line, err)
			continue
		}
		switch {
		case frame.Method != "" && len(frame.ID) > 0:
			h.serveSDKRequest(frame)
		case frame.Method != "":
			h.notesMu.Lock()
			h.notifications = append(h.notifications, hostNotification{Method: frame.Method, Params: frame.Params})
			h.notesMu.Unlock()
		case len(frame.ID) > 0:
			resp := hostResponse{Result: frame.Result}
			if frame.Error != nil {
				resp.Err = &hostError{Code: frame.Error.Code, Message: frame.Error.Message}
				if len(frame.Error.Data) > 0 {
					var data ProtocolErrorData
					if err := json.Unmarshal(frame.Error.Data, &data); err == nil {
						resp.Err.Data = data
					}
				}
			}
			var id int64
			if err := json.Unmarshal(frame.ID, &id); err != nil {
				h.recordStray(frame.ID, resp.Err)
				continue
			}
			h.pendingMu.Lock()
			ch := h.pending[id]
			delete(h.pending, id)
			h.pendingMu.Unlock()
			if ch == nil {
				h.recordStray(frame.ID, resp.Err)
				continue
			}
			ch <- resp
		}
	}
}

func (h *fakeHost) recordStray(id json.RawMessage, herr *hostError) {
	h.strayMu.Lock()
	defer h.strayMu.Unlock()
	h.strays = append(h.strays, strayResponse{ID: append(json.RawMessage(nil), id...), Error: herr})
}

// nextStray waits for one stray error response and returns it.
func (h *fakeHost) nextStray() strayResponse {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h.strayMu.Lock()
		if len(h.strays) > 0 {
			stray := h.strays[0]
			h.strays = h.strays[1:]
			h.strayMu.Unlock()
			return stray
		}
		h.strayMu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatalf("fake host: no stray response within 5s")
	return strayResponse{}
}

func (h *fakeHost) serveSDKRequest(frame hostFrame) {
	h.handlersMu.Lock()
	handler := h.handlers[frame.Method]
	h.requestLog[frame.Method] = append(h.requestLog[frame.Method], append(json.RawMessage(nil), frame.Params...))
	h.handlersMu.Unlock()
	var result any
	var herr *hostError
	if handler == nil {
		herr = &hostError{Code: CodeMethodNotFound, Message: "method not found: " + frame.Method}
	} else {
		result, herr = handler(frame.Params)
	}
	var out []byte
	if herr != nil {
		errorFrame := map[string]any{"code": herr.Code, "message": herr.Message}
		if herr.Data != nil {
			errorFrame["data"] = herr.Data
		}
		out, _ = json.Marshal(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(frame.ID), "error": errorFrame})
	} else {
		raw, _ := json.Marshal(result)
		out, _ = json.Marshal(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(frame.ID), "result": json.RawMessage(raw)})
	}
	h.writeLine(out)
}

// onRequest installs the scripted answerer for one Extension → Host method.
func (h *fakeHost) onRequest(method string, handler func(params json.RawMessage) (any, *hostError)) {
	h.handlersMu.Lock()
	defer h.handlersMu.Unlock()
	h.handlers[method] = handler
}

// lastRawParams returns the raw params of the most recent Extension → Host
// request for method.
func (h *fakeHost) lastRawParams(t *testing.T, method string) json.RawMessage {
	t.Helper()
	h.handlersMu.Lock()
	defer h.handlersMu.Unlock()
	log := h.requestLog[method]
	if len(log) == 0 {
		t.Fatalf("fake host: no %s request recorded", method)
	}
	return log[len(log)-1]
}

// request sends one Host → Extension request and waits for its response.
func (h *fakeHost) request(method string, params any) hostResponse {
	_, ch := h.startRequest(method, params)
	select {
	case resp := <-ch:
		return resp
	case <-time.After(10 * time.Second):
		h.t.Fatalf("fake host: no response to %s", method)
		return hostResponse{}
	}
}

// startRequest sends one Host → Extension request without waiting; the
// response arrives on the returned channel.
func (h *fakeHost) startRequest(method string, params any) (int64, chan hostResponse) {
	h.pendingMu.Lock()
	h.nextID++
	id := h.nextID
	ch := make(chan hostResponse, 1)
	h.pending[id] = ch
	h.pendingMu.Unlock()
	raw, _ := json.Marshal(params)
	frame, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": method, "params": json.RawMessage(raw),
	})
	h.writeLine(frame)
	return id, ch
}

// notify sends one Host → Extension notification.
func (h *fakeHost) notify(method string, params any) {
	raw, _ := json.Marshal(params)
	frame, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": json.RawMessage(raw)})
	h.writeLine(frame)
}

// writeRaw sends one unvalidated frame, for strict-frame violation tests.
func (h *fakeHost) writeRaw(frame []byte) { h.writeLine(frame) }

func (h *fakeHost) writeLine(frame []byte) {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	if _, err := h.toSDK.Write(append(frame, '\n')); err != nil {
		h.t.Errorf("fake host: write to SDK: %v", err)
	}
}

// handshake runs the standard initialize + initialized sequence and returns
// the decoded initialize result.
func (h *fakeHost) handshake(t *testing.T) InitializeResult {
	t.Helper()
	resp := h.request(MethodExtensionInitialize, InitializeParams{
		ProtocolVersion: ProtocolVersion,
		ProtocolID:      ProtocolID,
		Manifest:        ManifestExpectation{Intercepts: InterceptEvents(), Capabilities: []string{"providers", "ui"}},
		Session:         SessionContext{SessionID: "sess-1", WorkspaceRoot: "/repo", Generation: 7},
		Capabilities:    HostCapabilities{ContentRefs: true, UIHost: UIHostHeadless, ProtocolVersion: ProtocolVersion},
	})
	if resp.Err != nil {
		t.Fatalf("initialize failed: %+v", resp.Err)
	}
	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode initialize result: %v", err)
	}
	h.notify(MethodExtensionInitialized, InitializedParams{})
	return result
}

// nextNotification waits for one SDK notification with the given method.
func (h *fakeHost) nextNotification(method string) hostNotification {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h.notesMu.Lock()
		for i, note := range h.notifications {
			if note.Method == method {
				h.notifications = append(h.notifications[:i], h.notifications[i+1:]...)
				h.notesMu.Unlock()
				return note
			}
		}
		h.notesMu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatalf("fake host: no %s notification within 5s", method)
	return hostNotification{}
}

// notificationsSnapshot returns all recorded SDK notifications.
func (h *fakeHost) notificationsSnapshot() []hostNotification {
	h.notesMu.Lock()
	defer h.notesMu.Unlock()
	return append([]hostNotification(nil), h.notifications...)
}

// streamNotifications returns all stream/chunk and stream/end notifications
// recorded so far, in arrival order, decoded.
func (h *fakeHost) streamNotifications() (chunks []StreamChunkParams, ends []StreamEndParams) {
	for _, note := range h.notificationsSnapshot() {
		switch note.Method {
		case MethodExtensionProviderStreamChunk:
			var p StreamChunkParams
			if err := json.Unmarshal(note.Params, &p); err == nil {
				chunks = append(chunks, p)
			}
		case MethodExtensionProviderStreamEnd:
			var p StreamEndParams
			if err := json.Unmarshal(note.Params, &p); err == nil {
				ends = append(ends, p)
			}
		}
	}
	return chunks, ends
}

// waitStreamEnd polls until one stream/end notification arrives and returns
// it decoded. It does not consume anything.
func (h *fakeHost) waitStreamEnd() StreamEndParams {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, ends := h.streamNotifications()
		if len(ends) > 0 {
			return ends[len(ends)-1]
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatalf("fake host: no stream/end within 5s")
	return StreamEndParams{}
}

// testHandler is a Handler returning a fixed declaration.
type testHandler struct {
	result *InitializeResult
	err    error
	seen   *InitializeParams
}

func (h *testHandler) Initialize(_ context.Context, p InitializeParams) (*InitializeResult, error) {
	if h.seen != nil {
		*h.seen = p
	}
	if h.err != nil {
		return nil, h.err
	}
	return h.result, nil
}

// HandlerFunc adapts a function to the Handler interface.
type HandlerFunc func(ctx context.Context, p InitializeParams) (*InitializeResult, error)

// Initialize implements Handler.
func (f HandlerFunc) Initialize(ctx context.Context, p InitializeParams) (*InitializeResult, error) {
	return f(ctx, p)
}

func basicHandler() *testHandler {
	return &testHandler{result: &InitializeResult{
		Name: "test-ext", Version: "0.1.0",
		Subscriptions: []string{"tool.before"},
	}}
}

var errTest = errors.New("test error")
