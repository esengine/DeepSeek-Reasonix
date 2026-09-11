package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPTransportBufferedSubscriptionDoesNotBlockStartup(t *testing.T) {
	listenStarted := make(chan struct{})
	listenStopped := make(chan struct{})
	var started atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}

		switch req.Method {
		case "server/discover":
			writeHTTPRPCResult(w, req.ID, map[string]any{
				"supportedVersions": []string{"2026-07-28"},
				"capabilities": map[string]any{
					"tools":     map[string]any{"listChanged": true},
					"resources": map[string]any{"listChanged": true},
				},
				"_meta": map[string]any{
					"io.modelcontextprotocol/serverInfo": map[string]any{"name": "qmd-like", "version": "1"},
				},
			})
		case "subscriptions/listen":
			if started.CompareAndSwap(false, true) {
				close(listenStarted)
			}
			// qmd 2.8.3 converts the infinite Web Response to an arrayBuffer
			// before writing Node's response headers. Model that observable wire
			// behavior: the request arrived, but no headers are ever flushed.
			<-r.Context().Done()
			close(listenStopped)
		case "tools/list":
			writeHTTPRPCResult(w, req.ID, map[string]any{"tools": []map[string]any{{
				"name":        "query",
				"description": "Search local markdown.",
				"inputSchema": map[string]any{"type": "object"},
			}}})
		default:
			http.Error(w, "unknown method", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	host, tools, err := StartAll(ctx, []Spec{{
		Name: "qmd-like", Type: "http", URL: srv.URL, StartupTimeout: 750 * time.Millisecond,
	}})
	if err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if len(tools) != 1 || tools[0].Name() != "mcp__qmd-like__query" {
		host.Close()
		t.Fatalf("tools = %v, want [mcp__qmd-like__query]", names(tools))
	}
	select {
	case <-listenStarted:
	case <-time.After(time.Second):
		host.Close()
		t.Fatal("subscriptions/listen was not attempted")
	}

	host.Close()
	select {
	case <-listenStopped:
	case <-time.After(time.Second):
		t.Fatal("buffered subscriptions/listen request survived host close")
	}
}

func TestHTTPTransportAsyncSubscriptionStillRoutesNotifications(t *testing.T) {
	notificationSent := make(chan struct{})
	var sent atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}

		switch req.Method {
		case "server/discover":
			writeRawHTTPRPCResult(w, req.ID, map[string]any{
				"supportedVersions": []string{"2026-07-28"},
				"capabilities": map[string]any{
					"tools": map[string]any{"listChanged": true},
				},
				"_meta": map[string]any{
					"io.modelcontextprotocol/serverInfo": map[string]any{"name": "streaming", "version": "1"},
				},
			})
		case "subscriptions/listen":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			ack, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "method": "notifications/subscriptions/acknowledged",
				"params": map[string]any{
					"notifications": map[string]any{"toolsListChanged": true},
					"_meta":         map[string]any{"io.modelcontextprotocol/subscriptionId": json.RawMessage(req.ID)},
				},
			})
			changed, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "method": "notifications/tools/list_changed",
				"params": map[string]any{
					"_meta": map[string]any{"io.modelcontextprotocol/subscriptionId": json.RawMessage(req.ID)},
				},
			})
			fmt.Fprintf(w, "event: message\ndata: %s\n\nevent: message\ndata: %s\n\n", ack, changed)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			if sent.CompareAndSwap(false, true) {
				close(notificationSent)
			}
			<-r.Context().Done()
		case "tools/list":
			writeRawHTTPRPCResult(w, req.ID, map[string]any{"tools": []map[string]any{{
				"name": "query", "inputSchema": map[string]any{"type": "object"},
			}}})
		default:
			http.Error(w, "unknown method", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	transport, err := newHTTPTransport(Spec{Name: "streaming", Type: "http", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close()
	notificationReceived := make(chan json.RawMessage, 1)
	unregister := transport.registerNotification("notifications/tools/list_changed", func(params json.RawMessage) {
		notificationReceived <- params
	})
	defer unregister()

	if _, err := transport.call(t.Context(), "tools/list", map[string]any{}); err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	select {
	case <-notificationSent:
	case <-time.After(time.Second):
		t.Fatal("server did not flush the subscription notification")
	}
	select {
	case params := <-notificationReceived:
		if !strings.Contains(string(params), "subscriptionId") {
			t.Fatalf("notification params = %s, want subscription metadata", params)
		}
	case <-time.After(time.Second):
		t.Fatal("streamed tools/list_changed notification was not routed")
	}
}

func TestHTTPTransportStatelessSubscription404KeepsSessionUsable(t *testing.T) {
	listenServed := make(chan struct{})
	var discoverCount atomic.Int32
	var listCount atomic.Int32
	var listenReported atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}

		switch req.Method {
		case "server/discover":
			discoverCount.Add(1)
			writeRawHTTPRPCResult(w, req.ID, map[string]any{
				"supportedVersions": []string{"2026-07-28"},
				"capabilities": map[string]any{
					"tools": map[string]any{"listChanged": true},
				},
				"_meta": map[string]any{
					"io.modelcontextprotocol/serverInfo": map[string]any{"name": "stateless-404", "version": "1"},
				},
			})
		case "subscriptions/listen":
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("404 Not Found"))
			if listenReported.CompareAndSwap(false, true) {
				close(listenServed)
			}
		case "tools/list":
			listCount.Add(1)
			writeRawHTTPRPCResult(w, req.ID, map[string]any{"tools": []any{}})
		default:
			http.Error(w, "unknown method", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	transport, err := newHTTPTransport(Spec{Name: "stateless-404", Type: "http", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close()
	if _, err := transport.call(t.Context(), "tools/list", map[string]any{}); err != nil {
		t.Fatalf("first tools/list: %v", err)
	}
	select {
	case <-listenServed:
	case <-time.After(time.Second):
		t.Fatal("subscriptions/listen was not rejected")
	}
	if _, err := transport.call(t.Context(), "tools/list", map[string]any{}); err != nil {
		t.Fatalf("tools/list after subscriptions/listen 404: %v", err)
	}
	if got := discoverCount.Load(); got != 1 {
		t.Fatalf("server/discover count = %d, want one surviving stateless session", got)
	}
	if got := listCount.Load(); got != 2 {
		t.Fatalf("tools/list count = %d, want 2", got)
	}
	transport.mu.Lock()
	generations := transport.nextGeneration
	transport.mu.Unlock()
	if generations != 1 {
		t.Fatalf("supervisor generations = %d, want no rebuild after optional subscription 404", generations)
	}
}

func TestHTTPTransportPerCallJSONRPC4xxKeepsSession(t *testing.T) {
	var discoverCount atomic.Int32
	var toolCallCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}

		switch req.Method {
		case "server/discover":
			discoverCount.Add(1)
			writeRawHTTPRPCResult(w, req.ID, map[string]any{
				"supportedVersions": []string{"2026-07-28"},
				"capabilities":      map[string]any{"tools": map[string]any{}},
				"_meta": map[string]any{
					"io.modelcontextprotocol/serverInfo": map[string]any{"name": "request-error", "version": "1"},
				},
			})
		case "tools/list":
			writeRawHTTPRPCResult(w, req.ID, map[string]any{"tools": []any{}})
		case "subscriptions/listen":
			// Keep the optional SEP-2575 listener out of this test's failure
			// path so the HTTP 400 below is attributable to tools/call.
			writeRawHTTPRPCResult(w, req.ID, map[string]any{})
		case "tools/call":
			toolCallCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"error": map[string]any{
					"code": -32021, "message": "missing required client capability",
				},
			})
		default:
			http.Error(w, "unknown method", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	transport, err := newHTTPTransport(Spec{Name: "request-error", Type: "http", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close()
	if _, err := transport.call(t.Context(), "tools/list", map[string]any{}); err != nil {
		t.Fatalf("initial tools/list: %v", err)
	}
	if _, err := transport.call(t.Context(), "tools/call", map[string]any{
		"name": "requires-capability", "arguments": map[string]any{},
	}); err == nil || !strings.Contains(err.Error(), "missing required client capability") {
		t.Fatalf("tools/call error = %v, want the server's per-call JSON-RPC error", err)
	}
	if _, err := transport.call(t.Context(), "tools/list", map[string]any{}); err != nil {
		t.Fatalf("tools/list after per-call HTTP 400: %v", err)
	}
	if got := discoverCount.Load(); got != 1 {
		t.Fatalf("server/discover count = %d, want the original session to survive", got)
	}
	if got := toolCallCount.Load(); got != 1 {
		t.Fatalf("tools/call count = %d, want no replay of a rejected writer", got)
	}
	transport.mu.Lock()
	generations := transport.nextGeneration
	transport.mu.Unlock()
	if generations != 1 {
		t.Fatalf("supervisor generations = %d, want no rebuild for a per-call rejection", generations)
	}
}

func TestHTTPTransportUnsupportedProtocolClosesNegotiatedSession(t *testing.T) {
	deleteReceived := make(chan struct{})
	var deleteReported atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			if got := r.Header.Get("Mcp-Session-Id"); got != "unsupported-session" {
				t.Errorf("DELETE session ID = %q, want unsupported-session", got)
			}
			w.WriteHeader(http.StatusOK)
			if deleteReported.CompareAndSwap(false, true) {
				close(deleteReceived)
			}
			return
		}
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}

		switch req.Method {
		case "server/discover":
			writeRawHTTPRPCError(w, req.ID, -32601, "method not found")
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "unsupported-session")
			writeRawHTTPRPCResult(w, req.ID, map[string]any{
				"protocolVersion": "2099-01-01",
				"serverInfo":      map[string]any{"name": "unsupported", "version": "1"},
				"capabilities":    map[string]any{},
			})
		default:
			http.Error(w, "unexpected method", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	transport, err := newHTTPTransport(Spec{Name: "unsupported", Type: "http", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close()
	if _, err := transport.call(t.Context(), "tools/list", map[string]any{}); err == nil || !strings.Contains(err.Error(), "unsupported protocol version") {
		t.Fatalf("tools/list error = %v, want unsupported protocol version", err)
	}
	select {
	case <-deleteReceived:
	case <-time.After(time.Second):
		t.Fatal("failed protocol negotiation did not terminate the allocated server session")
	}
}

func TestHTTPTransportLegacySessionlessJSONPostOnly(t *testing.T) {
	var discoverCount atomic.Int32
	var initializeCount atomic.Int32
	var getCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			getCount.Add(1)
			w.Header().Set("Allow", http.MethodPost)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}

		switch req.Method {
		case "server/discover":
			discoverCount.Add(1)
			writeRawHTTPRPCError(w, req.ID, -32601, "method not found")
		case "initialize":
			initializeCount.Add(1)
			writeRawHTTPRPCResult(w, req.ID, map[string]any{
				"protocolVersion": "2025-11-25",
				"serverInfo":      map[string]any{"name": "legacy-post", "version": "1"},
				"capabilities":    map[string]any{"tools": map[string]any{}},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			writeRawHTTPRPCResult(w, req.ID, map[string]any{"tools": []map[string]any{{
				"name": "legacy_tool", "inputSchema": map[string]any{"type": "object"},
			}}})
		default:
			http.Error(w, "unknown method", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	host, tools, err := StartAll(t.Context(), []Spec{{Name: "legacy-post", Type: "http", URL: srv.URL}})
	if err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	defer host.Close()
	if got := names(tools); len(got) != 1 || got[0] != "mcp__legacy-post__legacy_tool" {
		t.Fatalf("tools = %v, want [mcp__legacy-post__legacy_tool]", got)
	}
	if got := discoverCount.Load(); got != 1 {
		t.Fatalf("server/discover count = %d, want one bounded modern probe", got)
	}
	if got := initializeCount.Load(); got != 1 {
		t.Fatalf("initialize count = %d, want one legacy fallback", got)
	}
	if got := getCount.Load(); got != 1 {
		t.Fatalf("standalone GET count = %d, want one optional probe accepted as HTTP 405", got)
	}
}
