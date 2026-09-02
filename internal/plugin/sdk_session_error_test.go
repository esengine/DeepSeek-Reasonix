package plugin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	mcpjsonrpc "github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

func TestApplicationJSONRPCSessionErrorsAreNotTransportLoss(t *testing.T) {
	for _, message := range []string{
		"session not found",
		"session missing",
		"session expired",
		"invalid session",
		"unknown session",
	} {
		err := fmt.Errorf("calling tool: %w", &mcpjsonrpc.Error{Code: -32042, Message: message})
		if isExplicitMCPSessionMissing(err) {
			t.Errorf("isExplicitMCPSessionMissing(%q) = true, want false without transport rejection", message)
		}
	}

	err := fmt.Errorf("calling tool: %w", &mcpjsonrpc.Error{Code: -32042, Message: "not found"})
	if isMCPHTTPNotFound(err) {
		t.Fatal("application JSON-RPC not-found error was classified as HTTP 404")
	}
}

func TestStructuredHTTP404SessionErrorIsTransportLoss(t *testing.T) {
	sessionErr := &mcpjsonrpc.Error{Code: -32042, Message: "Session not found"}
	rejectedErr := &mcpjsonrpc.Error{Code: -32005, Message: "rejected by transport"}
	err := fmt.Errorf("sending tools/call: %w: %w: Not Found", sessionErr, rejectedErr)
	if !isExplicitMCPSessionMissing(err) {
		t.Fatal("structured HTTP 404 session error was not classified as transport session loss")
	}
}

func TestHTTPTransportApplicationSessionErrorDoesNotReplayToolCall(t *testing.T) {
	var initializeCount atomic.Int32
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
			writeRawHTTPRPCError(w, req.ID, -32601, "method not found")
		case "initialize":
			initializeCount.Add(1)
			w.Header().Set("Mcp-Session-Id", "application-session")
			writeRawHTTPRPCResult(w, req.ID, map[string]any{
				"protocolVersion": testLegacyProtocolVersion,
				"serverInfo":      map[string]any{"name": "application-error", "version": "1"},
				"capabilities":    map[string]any{"tools": map[string]any{}},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			writeRawHTTPRPCResult(w, req.ID, map[string]any{"tools": []any{}})
		case "tools/call":
			toolCallCount.Add(1)
			writeRawHTTPRPCError(w, req.ID, -32042, "invalid session")
		default:
			http.Error(w, "unknown method", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	transport, err := newHTTPTransport(Spec{Name: "application-error", Type: "http", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close()
	if _, err := transport.call(t.Context(), "tools/list", map[string]any{}); err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if _, err := transport.call(t.Context(), "tools/call", map[string]any{
		"name": "write", "arguments": map[string]any{},
	}); err == nil || !strings.Contains(err.Error(), "invalid session") {
		t.Fatalf("tools/call error = %v, want application error", err)
	}
	if got := initializeCount.Load(); got != 1 {
		t.Fatalf("initialize count = %d, want no session rebuild", got)
	}
	if got := toolCallCount.Load(); got != 1 {
		t.Fatalf("tools/call count = %d, want no replay", got)
	}
}
