package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/tool"
)

// mcpHTTPServer is a minimal Streamable HTTP MCP server for tests. When sse is
// true it replies as text/event-stream (prefixing a server notification event
// to prove the client skips non-matching messages); otherwise application/json.
// It assigns a session id on initialize and fails any later request that
// doesn't echo it, and requires the Authorization header — so the test proves
// session + header plumbing, not just the happy path.
func mcpHTTPServer(t *testing.T, sse bool) *httptest.Server {
	t.Helper()
	const sessionID = "sess-xyz"
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		var req struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}

		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", sessionID)
		} else if got := r.Header.Get("Mcp-Session-Id"); got != sessionID {
			http.Error(w, "missing session id", http.StatusBadRequest)
			return
		}

		if req.ID == nil { // notification
			w.WriteHeader(http.StatusAccepted)
			return
		}

		var result any
		progressToken := ""
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": testLegacyProtocolVersion, "serverInfo": map[string]any{"name": "h", "version": "0"}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "greet",
				"description": "Greet someone.",
				"inputSchema": map[string]any{"type": "object"},
				"annotations": map[string]any{"readOnlyHint": true, "destructiveHint": true},
			}}}
		case "tools/call":
			var p struct {
				Meta      map[string]any `json:"_meta"`
				Arguments struct {
					Name string `json:"name"`
				} `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			progressToken, _ = p.Meta["progressToken"].(string)
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "hello " + p.Arguments.Name}}}
		}
		resp := map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result}
		b, _ := json.Marshal(resp)

		if sse {
			w.Header().Set("Content-Type", "text/event-stream")
			// A server notification first: the client must skip it and keep
			// reading for the id-matching response.
			fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{}}\n\n")
			if progressToken != "" {
				progress, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/progress", "params": map[string]any{
					"progressToken": progressToken, "progress": 3, "total": 4, "message": "Streaming",
				}})
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", progress)
			}
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
}

func runHTTPTransportTest(t *testing.T, sse bool) {
	srv := mcpHTTPServer(t, sse)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	host, tools, err := StartAll(ctx, []Spec{{
		Name:    "h",
		Type:    "http",
		URL:     srv.URL,
		Headers: map[string]string{"Authorization": "Bearer secret"},
	}})
	if err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	defer host.Close()

	if len(tools) != 1 || tools[0].Name() != "mcp__h__greet" {
		t.Fatalf("tools = %v, want [mcp__h__greet]", names(tools))
	}
	if !tools[0].ReadOnly() {
		t.Error("readOnlyHint not honoured over HTTP")
	}
	annotations, ok := tools[0].(tool.MCPAnnotations)
	if !ok || !annotations.MCPDestructiveHint() {
		t.Error("destructiveHint not honoured over HTTP")
	}
	progress := make(chan string, 1)
	executeCtx := tool.WithProgress(ctx, func(chunk string) { progress <- chunk })
	got, err := tools[0].Execute(executeCtx, json.RawMessage(`{"name":"sam"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got != "hello sam" {
		t.Errorf("Execute = %q, want %q", got, "hello sam")
	}
	if sse {
		select {
		case chunk := <-progress:
			if chunk != "Streaming (3/4)\n" {
				t.Fatalf("progress = %q", chunk)
			}
		case <-time.After(time.Second):
			t.Fatal("Streamable HTTP progress notification was not routed")
		}
	}
}

func TestHTTPTransportJSON(t *testing.T) { runHTTPTransportTest(t, false) }
func TestHTTPTransportSSE(t *testing.T)  { runHTTPTransportTest(t, true) }

func TestHTTPTransportDoesNotRedirectCredentialsAcrossOrigins(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		if got := r.Header.Get("X-API-Key"); got != "" {
			t.Errorf("redirect target received credential header %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/mcp", http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	transport, err := newHTTPTransport(Spec{
		Name: "redirect", Type: "http", URL: source.URL,
		Headers: map[string]string{"X-API-Key": "secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := transport.do(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("cross-origin redirect status = %d, want %d", resp.StatusCode, http.StatusTemporaryRedirect)
	}
	if targetCalls.Load() != 0 {
		t.Fatalf("cross-origin redirect target received %d requests", targetCalls.Load())
	}
}

func TestHTTPTransportDeleteHasBoundedCleanupContext(t *testing.T) {
	origin, err := url.Parse("https://mcp.example.test/stream")
	if err != nil {
		t.Fatal(err)
	}
	roundTripper := &sameOriginMCPRoundTripper{
		origin: origin,
		headers: map[string]string{
			"IJ_MCP_SERVER_PROJECT_PATH": "/redacted/project",
		},
		base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Header.Get("IJ_MCP_SERVER_PROJECT_PATH") == "" || request.Header.Get("Mcp-Session-Id") == "" {
				t.Error("DELETE did not carry the configured and session headers")
			}
			<-request.Context().Done()
			return nil, request.Context().Err()
		}),
	}
	request, err := http.NewRequest(http.MethodDelete, origin.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Mcp-Session-Id", "must-not-be-logged")
	started := time.Now()
	if _, err := roundTripper.RoundTrip(request); err == nil {
		t.Fatal("hanging DELETE unexpectedly succeeded")
	}
	if elapsed := time.Since(started); elapsed > 2500*time.Millisecond {
		t.Fatalf("hanging DELETE returned after %s, want <=2.5s", elapsed)
	}
}

func TestHTTPTransportDoesNotLoadOAuthStateWithStaticAPIKey(t *testing.T) {
	stateDir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-Key"); got != "configured" {
			t.Errorf("API key = %q, want configured", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("stale OAuth authorization leaked alongside static credentials: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer server.Close()
	if err := saveMCPOAuthState(stateDir, mcpOAuthState{
		Version: 1, Resource: server.URL, AccessToken: "stale-oauth", TokenType: "Bearer",
	}); err != nil {
		t.Fatal(err)
	}
	transport, err := newHTTPTransport(Spec{
		Name: "remote", Type: "http", URL: server.URL, StateDir: stateDir,
		Headers: map[string]string{"X-API-Key": "configured"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close()
	if transport.oauth != nil {
		t.Fatal("static authentication must disable Reasonix OAuth state")
	}
	resp, err := transport.do(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("transport status = %d, want 200", resp.StatusCode)
	}
}

func TestHTTPTransportReinitializesExpiredSession(t *testing.T) {
	var initializeCount atomic.Int32
	var toolCallCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}

		if req.Method == "initialize" {
			n := initializeCount.Add(1)
			w.Header().Set("Mcp-Session-Id", fmt.Sprintf("sess-%d", n))
			writeHTTPRPCResult(w, req.ID, map[string]any{
				"protocolVersion": testLegacyProtocolVersion,
				"serverInfo":      map[string]any{"name": "h", "version": "0"},
			})
			return
		}

		expectedSession := fmt.Sprintf("sess-%d", initializeCount.Load())
		if got := r.Header.Get("Mcp-Session-Id"); got != expectedSession {
			http.Error(w, "missing session id", http.StatusBadRequest)
			return
		}

		if req.ID == nil { // notifications/initialized
			w.WriteHeader(http.StatusAccepted)
			return
		}

		switch req.Method {
		case "tools/list":
			writeHTTPRPCResult(w, req.ID, map[string]any{"tools": []map[string]any{{
				"name":        "greet",
				"description": "Greet someone.",
				"inputSchema": map[string]any{"type": "object"},
			}}})
		case "tools/call":
			n := toolCallCount.Add(1)
			if n == 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"error":{"code":-32001,"message":"Session not found"}}`, *req.ID)
				return
			}
			if got := r.Header.Get("Mcp-Session-Id"); got != "sess-2" {
				http.Error(w, "retry did not use the new session", http.StatusBadRequest)
				return
			}
			writeHTTPRPCResult(w, req.ID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": "hello retry"}},
			})
		default:
			http.Error(w, "unknown method", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	host, tools, err := StartAll(ctx, []Spec{{Name: "h", Type: "http", URL: srv.URL}})
	if err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	defer host.Close()
	host.mu.RLock()
	client := host.clients[0]
	host.mu.RUnlock()

	done := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-done:
				return
			default:
				_, _ = client.capabilities.prompts, client.capabilities.resources
			}
		}
	}()
	defer func() {
		close(done)
		<-readerDone
	}()

	got, err := tools[0].Execute(ctx, json.RawMessage(`{"name":"sam"}`))
	if err != nil {
		t.Fatalf("Execute after expired session: %v", err)
	}
	if got != "hello retry" {
		t.Errorf("Execute = %q, want %q", got, "hello retry")
	}
	if got := initializeCount.Load(); got != 2 {
		t.Errorf("initialize count = %d, want 2", got)
	}
	if got := toolCallCount.Load(); got != 2 {
		t.Errorf("tools/call count = %d, want 2", got)
	}
}

func TestHTTPTransportReinitializesPlainAndEmptyExpiredSession(t *testing.T) {
	for _, test := range []struct {
		name        string
		contentType string
		body        string
	}{
		{name: "plain-text", contentType: "text/plain", body: "Streamable HTTP session not found"},
		{name: "empty-body"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var initializeCount atomic.Int32
			var toolCallCount atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				var req struct {
					ID     *int   `json:"id"`
					Method string `json:"method"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, "bad body", http.StatusBadRequest)
					return
				}
				if req.Method == "initialize" {
					n := initializeCount.Add(1)
					w.Header().Set("Mcp-Session-Id", fmt.Sprintf("session-%d", n))
					writeHTTPRPCResult(w, req.ID, map[string]any{
						"protocolVersion": testLegacyProtocolVersion,
						"serverInfo":      map[string]any{"name": "session-body", "version": "1"},
					})
					return
				}
				if req.ID == nil {
					w.WriteHeader(http.StatusAccepted)
					return
				}
				switch req.Method {
				case "tools/list":
					writeHTTPRPCResult(w, req.ID, map[string]any{"tools": []any{}})
				case "tools/call":
					if toolCallCount.Add(1) == 1 {
						if test.contentType != "" {
							w.Header().Set("Content-Type", test.contentType)
						}
						w.WriteHeader(http.StatusNotFound)
						_, _ = w.Write([]byte(test.body))
						return
					}
					writeHTTPRPCResult(w, req.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": "recovered"}}})
				default:
					http.Error(w, "unknown method", http.StatusBadRequest)
				}
			}))
			defer srv.Close()

			transport, err := newHTTPTransport(Spec{Name: "session-body", Type: "http", URL: srv.URL})
			if err != nil {
				t.Fatal(err)
			}
			defer transport.close()
			if _, err := transport.call(t.Context(), "tools/list", map[string]any{}); err != nil {
				t.Fatalf("tools/list: %v", err)
			}
			result, err := transport.call(t.Context(), "tools/call", map[string]any{"name": "work", "arguments": map[string]any{}})
			if err != nil {
				t.Fatalf("tools/call: %v", err)
			}
			if !containsJSONText(result, "recovered") {
				t.Fatalf("tools/call result = %s", result)
			}
			if got := initializeCount.Load(); got != 2 {
				t.Fatalf("initialize count = %d, want 2", got)
			}
			if got := toolCallCount.Load(); got != 2 {
				t.Fatalf("tools/call count = %d, want 2", got)
			}
		})
	}
}

func TestHTTPTransportSessionMissingConcurrentCallsShareOneRebuild(t *testing.T) {
	var initializeCount atomic.Int32
	var expiredCalls atomic.Int32
	var totalCalls atomic.Int32
	releaseExpired := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if req.Method == "initialize" {
			n := initializeCount.Add(1)
			w.Header().Set("Mcp-Session-Id", fmt.Sprintf("concurrent-%d", n))
			writeHTTPRPCResult(w, req.ID, map[string]any{
				"protocolVersion": testLegacyProtocolVersion,
				"serverInfo":      map[string]any{"name": "concurrent", "version": "1"},
			})
			return
		}
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		switch req.Method {
		case "tools/list":
			writeHTTPRPCResult(w, req.ID, map[string]any{"tools": []any{}})
		case "tools/call":
			totalCalls.Add(1)
			if r.Header.Get("Mcp-Session-Id") == "concurrent-1" {
				if expiredCalls.Add(1) == 2 {
					close(releaseExpired)
				}
				select {
				case <-releaseExpired:
				case <-r.Context().Done():
					return
				}
				http.Error(w, "Streamable HTTP session not found", http.StatusNotFound)
				return
			}
			writeHTTPRPCResult(w, req.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": "ok"}}})
		default:
			http.Error(w, "unknown method", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	transport, err := newHTTPTransport(Spec{Name: "concurrent", Type: "http", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close()
	if _, err := transport.call(t.Context(), "tools/list", map[string]any{}); err != nil {
		t.Fatalf("tools/list: %v", err)
	}

	errs := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := transport.call(t.Context(), "tools/call", map[string]any{"name": "work", "arguments": map[string]any{}})
			errs <- err
		}()
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent tools/call: %v", err)
		}
	}
	if got := initializeCount.Load(); got != 2 {
		t.Fatalf("initialize count = %d, want initial + one shared rebuild", got)
	}
	if got := expiredCalls.Load(); got != 2 {
		t.Fatalf("expired first-generation calls = %d, want 2", got)
	}
	if got := totalCalls.Load(); got != 4 {
		t.Fatalf("total tools/call requests = %d, want 2 failed + 2 replayed", got)
	}
}

func TestHTTPTransportSessionMissingWithoutSessionDoesNotLoop(t *testing.T) {
	var initializeCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			initializeCount.Add(1)
		}
		http.Error(w, "not an MCP endpoint", http.StatusNotFound)
	}))
	defer srv.Close()

	transport, err := newHTTPTransport(Spec{Name: "missing-endpoint", Type: "http", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close()
	if _, err := transport.call(t.Context(), "tools/list", map[string]any{}); err == nil {
		t.Fatal("tools/list unexpectedly succeeded")
	}
	// The SDK performs its bounded modern-to-legacy protocol discovery fallback,
	// but Reasonix must not treat a sessionless 404 as a lost established session
	// and start another supervisor generation.
	if got := initializeCount.Load(); got != 2 {
		t.Fatalf("initialize requests = %d, want the SDK's two bounded protocol probes", got)
	}
	transport.mu.Lock()
	generations := transport.nextGeneration
	transport.mu.Unlock()
	if generations != 1 {
		t.Fatalf("supervisor generations = %d, want no session rebuild", generations)
	}
}

func TestHTTPTransportSessionMissingReplaysAtMostOnce(t *testing.T) {
	var initializeCount atomic.Int32
	var toolCallCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if req.Method == "initialize" {
			n := initializeCount.Add(1)
			w.Header().Set("Mcp-Session-Id", fmt.Sprintf("missing-%d", n))
			writeHTTPRPCResult(w, req.ID, map[string]any{
				"protocolVersion": testLegacyProtocolVersion,
				"serverInfo":      map[string]any{"name": "missing", "version": "1"},
			})
			return
		}
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		switch req.Method {
		case "tools/list":
			writeHTTPRPCResult(w, req.ID, map[string]any{"tools": []any{}})
		case "tools/call":
			toolCallCount.Add(1)
			http.Error(w, "session gone", http.StatusNotFound)
		default:
			http.Error(w, "unknown method", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	transport, err := newHTTPTransport(Spec{Name: "missing", Type: "http", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close()
	if _, err := transport.call(t.Context(), "tools/list", map[string]any{}); err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if _, err := transport.call(t.Context(), "tools/call", map[string]any{"name": "work", "arguments": map[string]any{}}); err == nil {
		t.Fatal("tools/call unexpectedly succeeded")
	}
	if got := initializeCount.Load(); got != 2 {
		t.Fatalf("initialize count = %d, want 2", got)
	}
	if got := toolCallCount.Load(); got != 2 {
		t.Fatalf("tools/call count = %d, want exactly one replay", got)
	}
}

func TestHTTPTransportDoesNotReplayWriterAfterUnknownDisconnect(t *testing.T) {
	var toolCallCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "unknown-result")
			writeHTTPRPCResult(w, req.ID, map[string]any{
				"protocolVersion": testLegacyProtocolVersion,
				"serverInfo":      map[string]any{"name": "unknown", "version": "1"},
			})
			return
		}
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		switch req.Method {
		case "tools/list":
			writeHTTPRPCResult(w, req.ID, map[string]any{"tools": []any{}})
		case "tools/call":
			toolCallCount.Add(1)
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Error("response writer does not support hijacking")
				return
			}
			conn, _, err := hijacker.Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close()
		default:
			http.Error(w, "unknown method", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	transport, err := newHTTPTransport(Spec{Name: "unknown", Type: "http", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	transport.reconnectDelays = nil
	defer transport.close()
	if _, err := transport.call(t.Context(), "tools/list", map[string]any{}); err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	_, err = transport.call(t.Context(), "tools/call", map[string]any{"name": "work", "arguments": map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), "was not retried") {
		t.Fatalf("tools/call error = %v, want unknown-result non-replay error", err)
	}
	if got := toolCallCount.Load(); got != 1 {
		t.Fatalf("tools/call count = %d, want no replay", got)
	}
}

// TestHTTPTransportRPCError checks a JSON-RPC error response surfaces as an
// error rather than an empty result.
func TestHTTPTransportRPCError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID *int `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"error":{"code":-32000,"message":"boom"}}`, *req.ID)
	}))
	defer srv.Close()

	ctx := context.Background()
	_, _, err := StartAll(ctx, []Spec{{Name: "e", Type: "http", URL: srv.URL}})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want initialize to fail with rpc error, got %v", err)
	}
}

// TestSSETransportUnsupported documents that the legacy sse transport is
// recognised but deferred with a clear, actionable error.
func TestSSETransportUnsupported(t *testing.T) {
	_, _, err := StartAll(context.Background(), []Spec{{Name: "legacy", Type: "sse", URL: "http://x"}})
	if err == nil || !strings.Contains(err.Error(), "http") {
		t.Fatalf("sse should error pointing to http, got %v", err)
	}
}

func writeHTTPRPCResult(w http.ResponseWriter, id *int, result any) {
	if id == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	resp := map[string]any{"jsonrpc": "2.0", "id": *id, "result": result}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func writeRawHTTPRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeRawHTTPRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}

func names(ts []tool.Tool) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Name()
	}
	return out
}
