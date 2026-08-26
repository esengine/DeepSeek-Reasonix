package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestJetBrainsPendingSessionPromotedByStandaloneGET(t *testing.T) {
	const (
		sessionID   = "jetbrains-session-secret"
		projectPath = "/private/project-path"
	)
	var (
		mu            sync.Mutex
		active        bool
		virtualSecond int
		getCount      int
		notFoundCount int
		deleteCount   int
	)
	getReady := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("IJ_MCP_SERVER_PROJECT_PATH"); got != projectPath {
			http.Error(w, "missing project header", http.StatusBadRequest)
			return
		}
		if r.Method == http.MethodGet {
			if got := r.Header.Get("Mcp-Session-Id"); got != sessionID {
				http.Error(w, "missing session", http.StatusNotFound)
				return
			}
			mu.Lock()
			active = true
			getCount++
			if getCount == 1 {
				close(getReady)
			}
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		if r.Method == http.MethodDelete {
			if got := r.Header.Get("Mcp-Session-Id"); got != sessionID {
				http.Error(w, "missing session", http.StatusBadRequest)
				return
			}
			mu.Lock()
			deleteCount++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}

		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if request.Method == "server/discover" {
			writeRawHTTPRPCError(w, request.ID, -32601, "Method not found")
			return
		}
		if request.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", sessionID)
			writeRawHTTPRPCResult(w, request.ID, map[string]any{
				"protocolVersion": testLegacyProtocolVersion,
				"serverInfo":      map[string]any{"name": "jetbrains", "version": "1"},
				"capabilities":    map[string]any{"tools": map[string]any{}},
			})
			return
		}
		if len(request.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		mu.Lock()
		expiredPending := !active && virtualSecond >= 15
		if expiredPending {
			notFoundCount++
		}
		mu.Unlock()
		if expiredPending {
			http.Error(w, "Streamable HTTP session not found", http.StatusNotFound)
			return
		}
		switch request.Method {
		case "tools/list":
			writeRawHTTPRPCResult(w, request.ID, map[string]any{"tools": []map[string]any{{
				"name": "build_project", "description": "Build the project",
				"inputSchema": map[string]any{"type": "object"},
			}}})
		case "tools/call":
			writeRawHTTPRPCResult(w, request.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": "built"}}})
		default:
			writeRawHTTPRPCError(w, request.ID, -32601, "Method not found")
		}
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	host, tools, err := StartAll(ctx, []Spec{{
		Name: "rvb_monitor", Type: "streamable-http", URL: server.URL,
		Headers: map[string]string{"IJ_MCP_SERVER_PROJECT_PATH": projectPath},
	}})
	if err != nil {
		server.Close()
		t.Fatalf("StartAll: %v", err)
	}
	select {
	case <-getReady:
	default:
		host.Close()
		server.Close()
		t.Fatal("client became ready before establishing standalone GET/SSE")
	}
	mu.Lock()
	virtualSecond = 20
	mu.Unlock()
	result, err := tools[0].Execute(ctx, json.RawMessage(`{}`))
	if err != nil || result != "built" {
		host.Close()
		server.Close()
		t.Fatalf("build_project after virtual 20s = %q, %v", result, err)
	}
	host.Close()
	server.Close()
	mu.Lock()
	defer mu.Unlock()
	if getCount != 1 || notFoundCount != 0 || deleteCount != 1 {
		t.Fatalf("GET=%d 404=%d DELETE=%d, want 1/0/1", getCount, notFoundCount, deleteCount)
	}
}
