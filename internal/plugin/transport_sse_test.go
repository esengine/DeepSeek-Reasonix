package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/tool"
)

func TestLegacySSETransportSupportsRootsToolsAndProgress(t *testing.T) {
	workspaceRoot := t.TempDir()
	events := make(chan string, 16)
	serverErr := make(chan error, 4)
	var state struct {
		sync.Mutex
		initializeID int
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "event: endpoint\ndata: /messages?session=test\n\n")
		flusher.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case event := <-events:
				_, _ = fmt.Fprint(w, event)
				flusher.Flush()
			}
		}
	})
	mux.HandleFunc("/messages", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" || r.URL.Query().Get("session") != "test" {
			http.Error(w, "missing auth or session", http.StatusUnauthorized)
			return
		}
		var message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		emit := func(payload any) {
			body, _ := json.Marshal(payload)
			events <- "event: message\ndata: " + string(body) + "\n\n"
		}
		switch message.Method {
		case "initialize":
			var params struct {
				Capabilities map[string]json.RawMessage `json:"capabilities"`
			}
			_ = json.Unmarshal(message.Params, &params)
			if _, ok := params.Capabilities["roots"]; !ok {
				serverErr <- fmt.Errorf("initialize capabilities = %v, want roots", params.Capabilities)
			}
			var initializeID int
			_ = json.Unmarshal(message.ID, &initializeID)
			state.Lock()
			state.initializeID = initializeID
			state.Unlock()
			emit(map[string]any{"jsonrpc": "2.0", "id": "server-roots", "method": "roots/list"})
		case "notifications/initialized":
		case "tools/list":
			var id int
			_ = json.Unmarshal(message.ID, &id)
			emit(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
				"tools": []any{map[string]any{
					"name": "work", "description": "Do work", "inputSchema": map[string]any{"type": "object"},
				}},
			}})
		case "tools/call":
			var id int
			_ = json.Unmarshal(message.ID, &id)
			var params struct {
				Meta map[string]any `json:"_meta"`
			}
			_ = json.Unmarshal(message.Params, &params)
			token, _ := params.Meta["progressToken"].(string)
			if token == "" {
				serverErr <- fmt.Errorf("tools/call missing progressToken: %s", message.Params)
			}
			emit(map[string]any{"jsonrpc": "2.0", "method": "notifications/progress", "params": map[string]any{
				"progressToken": token, "progress": 1, "total": 2, "message": "Working",
			}})
			emit(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
				"content": []any{map[string]any{"type": "text", "text": "done"}},
			}})
		case "":
			if strings.TrimSpace(string(message.ID)) != `"server-roots"` {
				serverErr <- fmt.Errorf("unexpected server response id %s", message.ID)
				break
			}
			var result struct {
				Roots []mcpRoot `json:"roots"`
			}
			_ = json.Unmarshal(message.Result, &result)
			want := mcpRoots(workspaceRoot)
			if len(result.Roots) != 1 || result.Roots[0] != want[0] {
				serverErr <- fmt.Errorf("roots/list result = %+v, want %+v", result.Roots, want)
			}
			state.Lock()
			initializeID := state.initializeID
			state.Unlock()
			emit(map[string]any{"jsonrpc": "2.0", "id": initializeID, "result": map[string]any{
				"protocolVersion": protocolVersion,
				"serverInfo":      map[string]any{"name": "legacy", "version": "1"},
				"capabilities":    map[string]any{"tools": map[string]any{}},
			}})
		default:
			serverErr <- fmt.Errorf("unexpected method %q", message.Method)
		}
		w.WriteHeader(http.StatusAccepted)
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	host, tools, err := StartAll(ctx, []Spec{{
		Name:          "legacy",
		Type:          "sse",
		URL:           server.URL + "/sse",
		Headers:       map[string]string{"Authorization": "Bearer secret"},
		WorkspaceRoot: workspaceRoot,
	}})
	if err != nil {
		t.Fatalf("StartAll legacy SSE: %v", err)
	}
	defer host.Close()
	if len(tools) != 1 || tools[0].Name() != "mcp__legacy__work" {
		t.Fatalf("tools = %v", names(tools))
	}

	progress := make(chan string, 1)
	toolCtx := tool.WithProgress(ctx, func(chunk string) { progress <- chunk })
	result, err := tools[0].Execute(toolCtx, json.RawMessage(`{}`))
	if err != nil || result != "done" {
		t.Fatalf("Execute = %q, %v", result, err)
	}
	select {
	case got := <-progress:
		if got != "Working (1/2)\n" {
			t.Fatalf("progress = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("legacy SSE progress was not routed")
	}
	select {
	case err := <-serverErr:
		t.Fatal(err)
	default:
	}
}

func TestLegacySSERejectsCrossOriginEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: endpoint\ndata: https://other.example/messages\n\n")
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	transport, err := newSSETransport(ctx, Spec{Name: "unsafe", URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close()
	_, err = transport.call(ctx, "initialize", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "cross-origin endpoint") {
		t.Fatalf("cross-origin endpoint error = %v", err)
	}
}

func TestLegacySSEBoundsConcurrentServerRequestReplies(t *testing.T) {
	events := make(chan string, 2*sseReplyQueueBound+2)
	releasePosts := make(chan struct{})
	postStarted := make(chan struct{})
	var firstPost sync.Once
	var activePosts atomic.Int32
	var maxPosts atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: endpoint\ndata: /messages\n\n")
		flusher.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case event := <-events:
				_, _ = fmt.Fprint(w, event)
				flusher.Flush()
			}
		}
	})
	mux.HandleFunc("/messages", func(w http.ResponseWriter, r *http.Request) {
		active := activePosts.Add(1)
		defer activePosts.Add(-1)
		for {
			seen := maxPosts.Load()
			if active <= seen || maxPosts.CompareAndSwap(seen, active) {
				break
			}
		}
		firstPost.Do(func() { close(postStarted) })
		select {
		case <-releasePosts:
			w.WriteHeader(http.StatusAccepted)
		case <-r.Context().Done():
		}
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	transport, err := newSSETransport(ctx, Spec{Name: "bounded", Type: "sse", URL: server.URL + "/sse"})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close()
	defer close(releasePosts)
	if err := transport.waitEndpoint(ctx); err != nil {
		t.Fatal(err)
	}

	waiting := make(chan rpcResponse, 1)
	transport.mu.Lock()
	transport.pending[7] = waiting
	transport.mu.Unlock()
	for i := 0; i < 2*sseReplyQueueBound; i++ {
		events <- fmt.Sprintf("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":\"srv-%d\",\"method\":\"ping\"}\n\n", i)
	}
	events <- "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{}}\n\n"

	select {
	case response := <-waiting:
		if response.ID != 7 {
			t.Fatalf("routed response id = %d, want 7", response.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SSE reader stopped routing responses while a reply POST was blocked")
	}
	select {
	case <-postStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE reply worker did not start its first POST")
	}
	if got := maxPosts.Load(); got != 1 {
		t.Fatalf("concurrent reply POSTs = %d, want 1", got)
	}
}
