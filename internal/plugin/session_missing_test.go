package plugin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestHTTPTransportEstablishedSessionless404DoesNotRebuild(t *testing.T) {
	var initializeCount atomic.Int32
	var listCount atomic.Int32
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
		switch req.Method {
		case "initialize":
			initializeCount.Add(1)
			writeHTTPRPCResult(w, req.ID, map[string]any{
				"protocolVersion": testLegacyProtocolVersion,
				"serverInfo":      map[string]any{"name": "sessionless", "version": "1"},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			listCount.Add(1)
			http.Error(w, "not found", http.StatusNotFound)
		default:
			http.Error(w, "unknown method", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	transport, err := newHTTPTransport(Spec{Name: "sessionless", Type: "http", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close()
	if _, err := transport.call(t.Context(), "tools/list", map[string]any{}); err == nil {
		t.Fatal("tools/list unexpectedly succeeded")
	}
	if got := initializeCount.Load(); got != 1 {
		t.Fatalf("initialize count = %d, want no session rebuild", got)
	}
	if got := listCount.Load(); got != 1 {
		t.Fatalf("tools/list count = %d, want no replay", got)
	}
	transport.mu.Lock()
	generations := transport.nextGeneration
	transport.mu.Unlock()
	if generations != 1 {
		t.Fatalf("supervisor generations = %d, want 1", generations)
	}
	diagnostics := transport.sessionDiagnostics()
	if diagnostics.LastErrorKind != SessionErrorProtocol {
		t.Fatalf("last error kind = %q, want %q", diagnostics.LastErrorKind, SessionErrorProtocol)
	}
}
