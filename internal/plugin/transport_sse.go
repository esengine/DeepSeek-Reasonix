package plugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"reasonix/internal/tool"
)

// sseTransport implements MCP's legacy HTTP+SSE transport. The client keeps a
// long-lived GET stream open; the server announces a POST endpoint through an
// `event: endpoint` frame, and all JSON-RPC messages travel to that endpoint
// while responses and server messages return on the GET stream.
type sseTransport struct {
	name         string
	getURL       *url.URL
	headers      map[string]string
	client       *http.Client
	roots        []mcpRoot
	progress     progressRouter
	replies      chan inboundMessage
	replyTimeout time.Duration

	ctx    context.Context
	cancel context.CancelFunc

	callMu        sync.Mutex
	mu            sync.Mutex
	nextID        int
	pending       map[int]chan rpcResponse
	readErr       error
	endpoint      *url.URL
	endpointErr   error
	endpointReady chan struct{}
	endpointOnce  sync.Once
	closeOnce     sync.Once
}

// sseReplyQueueBound keeps a server-request flood from creating an unbounded
// number of blocked HTTP POST goroutines. Overflow is intentionally dropped so
// the GET reader can continue routing client responses and notifications.
const sseReplyQueueBound = 16

func newSSETransport(ctx context.Context, s Spec) (*sseTransport, error) {
	if strings.TrimSpace(s.URL) == "" {
		return nil, fmt.Errorf("sse plugin %q: url is required", s.Name)
	}
	getURL, err := url.Parse(s.URL)
	if err != nil || getURL.Scheme == "" || getURL.Host == "" {
		return nil, fmt.Errorf("sse plugin %q: invalid url %q", s.Name, s.URL)
	}
	headers := make(map[string]string, len(s.Headers))
	for key, value := range s.Headers {
		headers[key] = value
	}
	lifeCtx, cancel := context.WithCancel(ctx)
	t := &sseTransport{
		name:          s.Name,
		getURL:        getURL,
		headers:       headers,
		roots:         mcpRoots(s.WorkspaceRoot),
		replies:       make(chan inboundMessage, sseReplyQueueBound),
		replyTimeout:  s.CallTimeout,
		ctx:           lifeCtx,
		cancel:        cancel,
		pending:       map[int]chan rpcResponse{},
		endpointReady: make(chan struct{}),
	}
	if t.replyTimeout <= 0 {
		t.replyTimeout = s.DefaultCallTimeout
	}
	if t.replyTimeout <= 0 {
		t.replyTimeout = defaultCallTimeout
	}
	t.client = &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) == 0 || sameHTTPOrigin(via[0].URL, req.URL) {
			return nil
		}
		return http.ErrUseLastResponse
	}}
	go t.replyLoop()
	go t.readLoop()
	return t, nil
}

func (t *sseTransport) registerProgress(token string, sink tool.ProgressFunc) func() {
	return t.progress.registerProgress(token, sink)
}

func (t *sseTransport) readLoop() {
	defer close(t.replies)
	req, err := http.NewRequestWithContext(t.ctx, http.MethodGet, t.getURL.String(), nil)
	if err != nil {
		t.fail(err)
		return
	}
	req.Header.Set("Accept", "text/event-stream")
	for key, value := range t.headers {
		req.Header.Set(key, value)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		t.fail(err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		t.fail(fmt.Errorf("GET %s: http %d: %s", t.getURL, resp.StatusCode, strings.TrimSpace(string(body))))
		return
	}
	if !strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		t.fail(fmt.Errorf("GET %s: expected text/event-stream, got %q", t.getURL, resp.Header.Get("Content-Type")))
		return
	}

	baseURL := resp.Request.URL
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxHTTPBody)
	eventName := "message"
	var data strings.Builder
	dispatch := func() {
		if data.Len() == 0 {
			eventName = "message"
			return
		}
		payload := data.String()
		data.Reset()
		t.handleEvent(eventName, payload, baseURL)
		eventName = "message"
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			dispatch()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if value, ok := strings.CutPrefix(line, "event:"); ok {
			eventName = strings.TrimSpace(value)
			continue
		}
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(value, " "))
		}
	}
	dispatch()
	if err := scanner.Err(); err != nil {
		t.fail(fmt.Errorf("read SSE: %w", err))
		return
	}
	t.fail(io.EOF)
}

func (t *sseTransport) handleEvent(eventName, payload string, baseURL *url.URL) {
	switch eventName {
	case "endpoint":
		endpoint, err := url.Parse(strings.TrimSpace(payload))
		if err == nil {
			endpoint = baseURL.ResolveReference(endpoint)
			if !sameHTTPOrigin(baseURL, endpoint) {
				err = fmt.Errorf("server announced cross-origin endpoint %s", endpoint)
			}
		}
		t.setEndpoint(endpoint, err)
	case "message", "":
		t.handleMessage([]byte(payload))
	}
}

func (t *sseTransport) setEndpoint(endpoint *url.URL, err error) {
	set := false
	t.endpointOnce.Do(func() {
		t.mu.Lock()
		t.endpoint = endpoint
		t.endpointErr = err
		t.mu.Unlock()
		close(t.endpointReady)
		set = true
	})
	if set && err != nil {
		t.failPending(err)
	}
}

func (t *sseTransport) handleMessage(payload []byte) {
	message, ok := decodeInboundMessage(payload)
	if !ok {
		return
	}
	if message.Method != "" {
		if isNotificationID(message.ID) {
			if message.Method == "notifications/progress" {
				t.progress.dispatchProgress(message.Params)
			}
			return
		}
		select {
		case t.replies <- message:
		default:
			// Let the remote server time out excess requests. Blocking here could
			// prevent an unrelated client response from ever reaching its caller.
		}
		return
	}
	var response rpcResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return
	}
	t.mu.Lock()
	ch := t.pending[response.ID]
	delete(t.pending, response.ID)
	t.mu.Unlock()
	if ch != nil {
		ch <- response
	}
}

// replyLoop serializes server-request responses through one bounded worker.
// Each POST has a finite deadline inherited from the server's call-timeout
// configuration; after a transport error the worker keeps draining so the SSE
// reader never waits on the queue.
func (t *sseTransport) replyLoop() {
	dead := false
	for message := range t.replies {
		if dead {
			continue
		}
		body, err := json.Marshal(serverRequestReply(message.ID, message.Method, t.roots))
		if err == nil {
			ctx, cancel := context.WithTimeout(t.ctx, t.replyTimeout)
			err = t.post(ctx, body)
			cancel()
		}
		if err != nil && t.ctx.Err() == nil {
			t.fail(fmt.Errorf("reply to %s: %w", message.Method, err))
			dead = true
		}
	}
}

func (t *sseTransport) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	t.callMu.Lock()
	defer t.callMu.Unlock()

	if err := t.waitEndpoint(ctx); err != nil {
		return nil, fmt.Errorf("plugin %q: %s: %w", t.name, method, err)
	}
	t.mu.Lock()
	if t.readErr != nil {
		err := t.readErr
		t.mu.Unlock()
		return nil, fmt.Errorf("plugin %q: %s: %w", t.name, method, err)
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

	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	if err := t.post(ctx, body); err != nil {
		return nil, fmt.Errorf("plugin %q: %s: %w", t.name, method, err)
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case response, ok := <-ch:
		if !ok {
			t.mu.Lock()
			err := t.readErr
			t.mu.Unlock()
			return nil, fmt.Errorf("plugin %q: %s: %w", t.name, method, err)
		}
		if response.Error != nil {
			return nil, fmt.Errorf("plugin %q: %w", t.name, response.Error)
		}
		return response.Result, nil
	}
}

func (t *sseTransport) notify(ctx context.Context, method string, params any) error {
	if err := t.waitEndpoint(ctx); err != nil {
		return fmt.Errorf("plugin %q: %s: %w", t.name, method, err)
	}
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	if err := t.post(ctx, body); err != nil {
		return fmt.Errorf("plugin %q: %s: %w", t.name, method, err)
	}
	return nil
}

func (t *sseTransport) waitEndpoint(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.endpointReady:
		t.mu.Lock()
		defer t.mu.Unlock()
		if t.endpointErr != nil {
			return t.endpointErr
		}
		if t.endpoint == nil {
			return errSSEEndpointMissing
		}
		return nil
	}
}

var errSSEEndpointMissing = errors.New("SSE stream ended before announcing an endpoint")

func (t *sseTransport) post(ctx context.Context, body []byte) error {
	t.mu.Lock()
	endpoint := t.endpoint
	t.mu.Unlock()
	if endpoint == nil {
		return errSSEEndpointMissing
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for key, value := range t.headers {
		req.Header.Set(key, value)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func (t *sseTransport) fail(err error) {
	t.endpointOnce.Do(func() {
		t.mu.Lock()
		t.endpointErr = err
		t.mu.Unlock()
		close(t.endpointReady)
	})
	t.failPending(err)
}

func (t *sseTransport) failPending(err error) {
	t.mu.Lock()
	if t.readErr == nil {
		t.readErr = err
	}
	for id, ch := range t.pending {
		close(ch)
		delete(t.pending, id)
	}
	t.mu.Unlock()
}

func (t *sseTransport) close() {
	t.closeOnce.Do(func() {
		t.cancel()
		t.client.CloseIdleConnections()
	})
}
