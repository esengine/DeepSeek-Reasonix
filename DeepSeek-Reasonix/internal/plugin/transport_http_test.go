package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": protocolVersion, "serverInfo": map[string]any{"name": "h", "version": "0"}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "greet",
				"description": "Greet someone.",
				"inputSchema": map[string]any{"type": "object"},
				"annotations": map[string]any{"readOnlyHint": true},
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

func names(ts []tool.Tool) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Name()
	}
	return out
}
