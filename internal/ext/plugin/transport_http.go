package plugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/mcpdiag"
	"reasonix/internal/contract/tool"
)

// maxHTTPBody caps how much of a JSON / SSE response body we read, so a
// misbehaving server can't make us buffer without bound.
const maxHTTPBody = 16 << 20 // 16 MiB

// httpTransport speaks MCP's Streamable HTTP transport: every JSON-RPC message
// is an HTTP POST to the server URL. The server replies with either
// application/json (one response) or text/event-stream (an SSE stream carrying
// the response plus any server notifications). The Mcp-Session-Id header, once
// the server assigns one, is echoed on every subsequent request.
//
// The mutex serialises a request and its response. That means concurrent tool
// calls to the *same* server run one at a time; calls to different servers use
// different transports and stay concurrent. Correctness over latency for P1 —
// it also keeps nextID and the session id race-free.
type httpTransport struct {
	name     string
	url      string
	headers  map[string]string
	client   *http.Client
	roots    []mcpRoot
	progress progressRouter
	oauth    *mcpOAuthClient

	mu      sync.Mutex
	nextID  int
	session httpSession
}

// httpSession is what the server issued at initialize: the Mcp-Session-Id it
// hands back, and the protocol revision the session agreed on.
type httpSession struct {
	id      string
	version string
}

func newHTTPTransport(s Spec) (*httpTransport, error) {
	if s.URL == "" {
		return nil, fmt.Errorf("http plugin %q: url is required", s.Name)
	}
	headers := make(map[string]string, len(s.Headers))
	maps.Copy(headers, s.Headers)
	var oauth *mcpOAuthClient
	var err error
	if !mcpdiag.HasAuthConfig(headers, s.Env, s.URL) {
		oauth, err = newMCPOAuthClient(s.StateDir, s.OAuthHTTPClient)
		if err != nil {
			return nil, fmt.Errorf("http plugin %q: load OAuth state: %w", s.Name, err)
		}
		if oauth != nil && !sameCanonicalResource(oauth.state.Resource, s.URL) {
			return nil, fmt.Errorf("http plugin %q: stored OAuth token belongs to a different MCP resource; clear authentication and authorize this endpoint", s.Name)
		}
	}
	return &httpTransport{
		name:    s.Name,
		url:     s.URL,
		headers: headers,
		roots:   mcpRoots(s.WorkspaceRoot),
		oauth:   oauth,
		client: &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) == 0 || sameHTTPOrigin(via[0].URL, req.URL) {
				return nil
			}
			// Do not send configured credentials to another origin. Returning
			// ErrUseLastResponse exposes the 3xx to the normal status handling
			// without issuing the redirected request.
			return http.ErrUseLastResponse
		}},
	}, nil
}

func sameHTTPOrigin(a, b *url.URL) bool {
	if a == nil || b == nil || !strings.EqualFold(a.Scheme, b.Scheme) || !strings.EqualFold(a.Hostname(), b.Hostname()) {
		return false
	}
	effectivePort := func(u *url.URL) string {
		if port := u.Port(); port != "" {
			return port
		}
		switch strings.ToLower(u.Scheme) {
		case "http":
			return "80"
		case "https":
			return "443"
		default:
			return ""
		}
	}
	return effectivePort(a) == effectivePort(b)
}

func (t *httpTransport) call(ctx context.Context, method string, params any) (result json.RawMessage, err error) {
	defer func() { err = secrets.DiagnosticError(err) }()
	id := t.nextRequestID()
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	modern := modernRequestHeaders(ctx, method, params)
	defer func() {
		// A modern request is cancelled by closing its stream, which the
		// context already did; the revision defines no cancel notification.
		if err != nil && ctx.Err() != nil && modern == nil {
			cancelInFlight(t, method, id, ctx.Err())
		}
	}()

	heldSession := t.sessionID() != ""
	resp, err := t.doOAuth(ctx, body, false, modern)
	if err != nil {
		return nil, fmt.Errorf("plugin %q: %s: %w", t.name, method, err)
	}
	defer resp.Body.Close()
	t.captureSession(resp)

	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		// The spec's rule, whatever the body says: a 404 to a request that
		// carried a session id means that session is gone.
		if resp.StatusCode == http.StatusNotFound && heldSession {
			t.clearSession()
			return nil, fmt.Errorf("plugin %q: %s: %w", t.name, method, &httpSessionExpiredError{
				status:    resp.StatusCode,
				bodyBytes: len(b),
			})
		}
		return nil, fmt.Errorf("plugin %q: %s: %w", t.name, method, &httpStatusError{Status: resp.StatusCode, BodyBytes: len(b), RPC: bodyRPCError(b)})
	}

	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return t.readSSEResponse(ctx, resp.Body, id)
	}
	return decodeRPCResult(resp.Body, t.name)
}

func (t *httpTransport) notify(ctx context.Context, method string, params any) error {
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	resp, err := t.do(ctx, body)
	if err != nil {
		return fmt.Errorf("plugin %q: %s: %w", t.name, method, err)
	}
	defer resp.Body.Close()
	t.captureSession(resp)
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxHTTPBody))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("plugin %q: %s: %w", t.name, method, &httpStatusError{Status: resp.StatusCode})
	}
	return nil
}

// close ends the server's session too, when there is one. Best effort: a
// server that does not allow DELETE answers 405, and the session then expires
// on its own.
func (t *httpTransport) close() {
	if sid := t.sessionID(); sid != "" {
		ctx, cancel := context.WithTimeout(context.Background(), cancelNotifyLimit)
		if req, err := http.NewRequestWithContext(ctx, http.MethodDelete, t.url, nil); err == nil {
			for k, v := range t.headers {
				req.Header.Set(k, v)
			}
			req.Header.Set("Mcp-Session-Id", sid)
			if resp, err := t.client.Do(req); err == nil {
				_ = resp.Body.Close()
			}
		}
		cancel()
	}
	t.client.CloseIdleConnections()
}

func (t *httpTransport) setProtocolVersion(version string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.session.version = version
}

func (t *httpTransport) registerProgress(token string, sink tool.ProgressFunc) func() {
	return t.progress.registerProgress(token, sink)
}

// mu covers the shared fields below and never a round trip: a request in flight
// must not stop the next call — or this one's own cancellation — from being
// written. Each request already carries its own context for that.
func (t *httpTransport) nextRequestID() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nextID++
	return t.nextID
}

func (t *httpTransport) sessionID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.session.id
}

func (t *httpTransport) protocolVersion() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.session.version
}

func (t *httpTransport) clearSession() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.session.id = ""
}

// do POSTs one JSON-RPC body with the standard MCP headers, the configured
// static headers, and the session id (once known).
func (t *httpTransport) do(ctx context.Context, body []byte) (*http.Response, error) {
	return t.doOAuth(ctx, body, false, nil)
}

func (t *httpTransport) doOAuth(ctx context.Context, body []byte, refreshed bool, modern http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return nil, secrets.DiagnosticError(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	// What actually goes out, kept so a refusal can name it. Re-reading the
	// store after the 401 would answer with whatever is there by then, and that
	// is exactly the credential another actor may have replaced it with.
	sent := ""
	usedOAuth := false
	if req.Header.Get("Authorization") == "" && t.oauth != nil {
		header, used, err := t.oauth.authorizationHeader(ctx)
		if err != nil {
			return nil, err
		}
		if header != "" {
			req.Header.Set("Authorization", header)
			usedOAuth = used
			sent = bearerToken(header)
		}
	}
	if sid := t.sessionID(); sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}
	if v := t.protocolVersion(); v != "" {
		req.Header.Set("MCP-Protocol-Version", v)
	}
	maps.Copy(req.Header, modern)
	resp, err := t.client.Do(req)
	if err != nil || refreshed || resp.StatusCode != http.StatusUnauthorized || !usedOAuth || !t.oauth.canRefresh() {
		return resp, secrets.DiagnosticError(err)
	}
	_ = resp.Body.Close()
	if _, _, err := t.oauth.authorizationHeaderAfterReject(ctx, sent); err != nil {
		return nil, err
	}
	return t.doOAuth(ctx, body, true, modern)
}

func (t *httpTransport) captureSession(resp *http.Response) {
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.mu.Lock()
		t.session.id = sid
		t.mu.Unlock()
	}
}

// httpStatusError is a non-2xx answer with the status kept as a number. The
// host knows which status it got, and formatting it into the sentence was where
// that stopped being true: everything downstream — a failure record, a settings
// row — was left reading the digits back out of prose an external server had a
// hand in writing.
type httpStatusError struct {
	Status    int
	BodyBytes int
	// RPC is the JSON-RPC error the body carried, when it carried one: a
	// modern server answers 400 with a typed error a caller has to tell apart.
	RPC *rpcError
}

func (e *httpStatusError) Unwrap() error {
	if e.RPC == nil {
		return nil
	}
	return e.RPC
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("http %d (response body omitted; read %d bytes)", e.Status, e.BodyBytes)
}

func (e *httpStatusError) DiagnosticFacts() string { return e.Error() }

type httpSessionExpiredError struct {
	status    int
	bodyBytes int
}

func (e *httpSessionExpiredError) Error() string {
	return fmt.Sprintf("http %d: MCP session expired (response body omitted; read %d bytes)", e.status, e.bodyBytes)
}

func (e *httpSessionExpiredError) DiagnosticFacts() string { return e.Error() }

// readSSEResponse scans an SSE stream for the JSON-RPC response matching id,
// skipping server notifications and any other-id messages. Per the SSE spec,
// consecutive data: lines within one event are joined with "\n" and an event is
// dispatched on the blank line that terminates it.
func (t *httpTransport) readSSEResponse(ctx context.Context, body io.Reader, id int) (json.RawMessage, error) {
	sc := bufio.NewScanner(io.LimitReader(body, maxHTTPBody))
	sc.Buffer(make([]byte, 0, 64*1024), maxHTTPBody)

	var data strings.Builder
	// match reports whether the accumulated event data is our response; it
	// returns (result, matched, error).
	match := func() (json.RawMessage, bool, error) {
		if data.Len() == 0 {
			return nil, false, nil
		}
		payload := data.String()
		data.Reset()
		message, ok := decodeInboundMessage([]byte(payload))
		if !ok {
			return nil, false, nil // not a JSON-RPC message we care about
		}
		if message.Method != "" {
			if isNotificationID(message.ID) {
				if message.Method == "notifications/progress" {
					t.progress.dispatchProgress(message.Params)
				}
				return nil, false, nil
			}
			if err := t.replyServerRequest(ctx, message); err != nil {
				return nil, false, err
			}
			return nil, false, nil
		}
		var resp rpcResponse
		if err := json.Unmarshal([]byte(payload), &resp); err != nil {
			return nil, false, nil
		}
		if resp.ID != id {
			return nil, false, nil // a notification or another call's response
		}
		if resp.Error != nil {
			return nil, false, fmt.Errorf("plugin %q: %w", t.name, resp.Error)
		}
		return resp.Result, true, nil
	}

	for sc.Scan() {
		line := sc.Text()
		if line == "" { // event boundary
			if res, ok, err := match(); err != nil || ok {
				return res, err
			}
			continue
		}
		if v, found := strings.CutPrefix(line, "data:"); found {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(v, " "))
		}
		// event:, id:, retry: and comments (":") are ignored
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("plugin %q: read SSE: %w", t.name, err)
	}
	if res, ok, err := match(); err != nil || ok { // stream ended on a final unterminated event
		return res, err
	}
	return nil, fmt.Errorf("plugin %q: SSE stream ended without a response to id %d", t.name, id)
}

// replyServerRequest sends a JSON-RPC response on a separate Streamable HTTP
// POST while the original response stream remains open.
func (t *httpTransport) replyServerRequest(ctx context.Context, message inboundMessage) error {
	reply := serverRequestReply(message.ID, message.Method, t.roots)
	if message.Method == elicitMethod {
		e, _ := tool.ElicitorFrom(ctx)
		reply = elicitationReply(ctx, e, t.name, message.ID, message.Params)
	}
	body, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	resp, err := t.do(ctx, body)
	if err != nil {
		return fmt.Errorf("plugin %q: reply to %s: %w", t.name, message.Method, err)
	}
	defer resp.Body.Close()
	t.captureSession(resp)
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxHTTPBody))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("plugin %q: reply to %s: %w", t.name, message.Method, &httpStatusError{Status: resp.StatusCode})
	}
	return nil
}

// decodeRPCResult parses a single application/json JSON-RPC response body.
func decodeRPCResult(body io.Reader, name string) (json.RawMessage, error) {
	b, err := io.ReadAll(io.LimitReader(body, maxHTTPBody))
	if err != nil {
		return nil, fmt.Errorf("plugin %q: read response: %w", name, err)
	}
	var resp rpcResponse
	if err := json.Unmarshal(bytes.TrimSpace(b), &resp); err != nil {
		return nil, fmt.Errorf("plugin %q: decode response: %w", name, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("plugin %q: %w", name, resp.Error)
	}
	return resp.Result, nil
}

// bodyRPCError reads a JSON-RPC error out of a non-2xx body, or nil.
func bodyRPCError(body []byte) *rpcError {
	var resp rpcResponse
	if json.Unmarshal(bytes.TrimSpace(body), &resp) != nil {
		return nil
	}
	return resp.Error
}
