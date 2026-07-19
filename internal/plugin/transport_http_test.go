package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/mcptrust"
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
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": protocolVersion, "serverInfo": map[string]any{"name": "h", "version": "0"}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "greet",
				"description": "Greet someone.",
				"inputSchema": map[string]any{"type": "object"},
				"annotations": map[string]any{"readOnlyHint": true, "destructiveHint": true},
			}}}
		case "tools/call":
			var p struct {
				Arguments struct {
					Name string `json:"name"`
				} `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "hello " + p.Arguments.Name}}}
		}
		resp := map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result}
		b, _ := json.Marshal(resp)

		if sse {
			w.Header().Set("Content-Type", "text/event-stream")
			// A server notification first: the client must skip it and keep
			// reading for the id-matching response.
			fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{}}\n\n")
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
	got, err := tools[0].Execute(ctx, json.RawMessage(`{"name":"sam"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got != "hello sam" {
		t.Errorf("Execute = %q, want %q", got, "hello sam")
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
				"protocolVersion": protocolVersion,
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
				_, _ = client.hasPrompts, client.hasResources
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

func names(ts []tool.Tool) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Name()
	}
	return out
}

// TestHTTPStartMigratesLegacyURLReceiptBeforeIdentityBlock covers the real
// eager/cache-miss startup chain: the pre-start identity gate must migrate a
// receipt still carrying the legacy URL fingerprint instead of blocking the
// server, credential rotation must keep starting, and a genuine resource
// scope change must still block before any network handshake.
func TestHTTPStartMigratesLegacyURLReceiptBeforeIdentityBlock(t *testing.T) {
	redirectCache(t)
	srv := mcpHTTPServer(t, false)
	defer srv.Close()

	manager := mcptrust.NewManager(filepath.Join(t.TempDir(), mcptrust.StateFilename), t.TempDir())
	spec := Spec{
		Name: "h", Type: "http",
		URL:          srv.URL + "?access_token=first&workspace=alpha",
		Headers:      map[string]string{"Authorization": "Bearer secret"},
		ConfigSource: "workspace_config", TrustManager: manager,
	}
	legacyFP, ok := legacySpecIdentityFingerprint(spec)
	if !ok {
		t.Fatal("legacy identity fingerprint unavailable for credential-bearing URL")
	}
	caps := []mcptrust.Capability{{RawName: "greet", ModelName: "mcp__h__greet", ReadOnly: true}}
	if err := manager.Trust(mcptrust.ScopeWorkspace, mcptrust.SourceUser, "h", "workspace_config", legacyFP, "", caps); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	host, tools, err := StartAll(ctx, []Spec{spec})
	if err != nil {
		t.Fatalf("eager start with a legacy receipt was blocked: %v", err)
	}
	defer host.Close()
	if len(tools) != 1 || tools[0].Name() != "mcp__h__greet" {
		t.Fatalf("tools = %v, want [mcp__h__greet]", names(tools))
	}
	current, err := specIdentityFingerprint(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, changed, err := manager.IdentityChanged("h", "workspace_config", current); err != nil || changed {
		t.Fatalf("receipt did not migrate at the pre-start gate: changed=%v err=%v", changed, err)
	}

	// Credential rotation keeps the migrated identity and keeps starting.
	rotated := spec
	rotated.URL = srv.URL + "?access_token=second&workspace=alpha"
	host2, _, err := StartAll(ctx, []Spec{rotated})
	if err != nil {
		t.Fatalf("credential rotation blocked startup: %v", err)
	}
	host2.Close()

	// A moved resource scope is a genuine identity change and must still block
	// before any process or network startup.
	moved := spec
	moved.URL = srv.URL + "?access_token=first&workspace=beta"
	if _, _, err := StartAll(ctx, []Spec{moved}); err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("workspace change start = %v, want identity-changed block", err)
	}
}
