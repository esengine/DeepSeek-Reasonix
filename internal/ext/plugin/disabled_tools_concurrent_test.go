package plugin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestDisabledToolsBindConcurrentAddWaiters(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	releaseHandshake := sync.OnceFunc(func() { close(release) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			close(started)
			<-release
			result = map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]any{"name": "filtered", "version": "1"}, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{"name": "write", "description": "Write", "inputSchema": map[string]any{"type": "object"}}}}
		default:
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result})
	}))
	defer server.Close()
	host := NewHost()
	defer host.Close()
	defer releaseHandshake()
	wide := Spec{Name: "filtered", Type: "http", URL: server.URL}
	owner := make(chan error, 1)
	go func() { _, err := host.Add(t.Context(), wide); owner <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("owner never started")
	}
	narrow := wide
	narrow.DisabledTools = []string{"write"}
	timer := time.AfterFunc(100*time.Millisecond, releaseHandshake)
	defer timer.Stop()
	if tools, err := host.Add(t.Context(), narrow); err == nil {
		t.Fatalf("concurrent waiter received another policy's tools: %v", tools)
	}
	if err := <-owner; err != nil {
		t.Fatalf("owner: %v", err)
	}
}
