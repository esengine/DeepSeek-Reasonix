package plugin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPTransportCloseCancelsActiveRequest(t *testing.T) {
	requestStarted := make(chan struct{})
	requestCancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "close-active")
			writeHTTPRPCResult(w, req.ID, map[string]any{
				"protocolVersion": testLegacyProtocolVersion,
				"serverInfo":      map[string]any{"name": "close-active", "version": "1"},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			writeHTTPRPCResult(w, req.ID, map[string]any{"tools": []any{}})
		case "tools/call":
			close(requestStarted)
			<-r.Context().Done()
			close(requestCancelled)
		default:
			http.Error(w, "unknown method", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	transport, err := newHTTPTransport(Spec{Name: "close-active", Type: "http", URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.call(t.Context(), "tools/list", map[string]any{}); err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	callDone := make(chan error, 1)
	go func() {
		_, err := transport.call(t.Context(), "tools/call", map[string]any{"name": "work", "arguments": map[string]any{}})
		callDone <- err
	}()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("tools/call did not reach the server")
	}

	closed := make(chan struct{})
	go func() {
		transport.close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(4 * time.Second):
		t.Fatal("transport close did not finish within its cleanup budget")
	}
	select {
	case <-requestCancelled:
	case <-time.After(time.Second):
		t.Fatal("transport close did not cancel the active HTTP request")
	}
	select {
	case err := <-callDone:
		if err == nil {
			t.Fatal("cancelled tools/call unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled tools/call did not return")
	}
}
