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
	toolListRefreshed := make(chan struct{}, 1)
	var toolListCalls atomic.Int32
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
		case "server/discover":
			emit(map[string]any{"jsonrpc": "2.0", "id": message.ID, "error": map[string]any{
				"code": -32601, "message": "Method not found",
			}})
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
			if toolListCalls.Add(1) > 1 {
				toolListRefreshed <- struct{}{}
			}
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
			emit(map[string]any{"jsonrpc": "2.0", "method": "notifications/tools/list_changed"})
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
				"protocolVersion": testLegacyProtocolVersion,
				"serverInfo":      map[string]any{"name": "legacy", "version": "1"},
				"capabilities":    map[string]any{"tools": map[string]any{"listChanged": true}},
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
	toolsChanged := make(chan struct{}, 1)
	unsubscribe := host.SubscribeToolListChanges(ctx, func(spec Spec, tools []tool.Tool) {
		if spec.Name == "legacy" && len(tools) == 1 {
			toolsChanged <- struct{}{}
		}
	})
	defer unsubscribe()

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
	case <-toolListRefreshed:
	case <-time.After(time.Second):
		t.Fatal("legacy SSE tools/list_changed notification was not routed")
	}
	select {
	case <-toolsChanged:
		t.Fatal("unchanged tool catalog should not publish a registry update")
	default:
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
	if err == nil {
		t.Fatal("cross-origin endpoint unexpectedly connected")
	}
}

type acknowledgedSSEEvent struct {
	payload string
	done    chan struct{}
}

func TestLegacySSEDisconnectRebuildsBeforeNextCall(t *testing.T) {
	var connections atomic.Int32
	var streamsMu sync.Mutex
	streams := make(map[int]chan acknowledgedSSEEvent)
	firstClosed := make(chan struct{})
	var closeFirst sync.Once

	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		generation := int(connections.Add(1))
		events := make(chan acknowledgedSSEEvent, 8)
		streamsMu.Lock()
		streams[generation] = events
		streamsMu.Unlock()
		defer func() {
			streamsMu.Lock()
			delete(streams, generation)
			streamsMu.Unlock()
		}()

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "event: endpoint\ndata: /messages?generation=%d\n\n", generation)
		flusher.Flush()
		closeSignal := (<-chan struct{})(firstClosed)
		if generation != 1 {
			closeSignal = nil
		}
		for {
			select {
			case <-r.Context().Done():
				return
			case <-closeSignal:
				return
			case event := <-events:
				_, _ = fmt.Fprint(w, event.payload)
				flusher.Flush()
				close(event.done)
			}
		}
	})
	mux.HandleFunc("/messages", func(w http.ResponseWriter, r *http.Request) {
		generation := 0
		_, _ = fmt.Sscanf(r.URL.Query().Get("generation"), "%d", &generation)
		streamsMu.Lock()
		events := streams[generation]
		streamsMu.Unlock()
		if events == nil {
			http.Error(w, "missing stream", http.StatusGone)
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
		if len(request.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
		switch request.Method {
		case "server/discover":
			response["error"] = map[string]any{"code": -32601, "message": "Method not found"}
		case "initialize":
			response["result"] = map[string]any{
				"protocolVersion": testLegacyProtocolVersion,
				"serverInfo":      map[string]any{"name": "disconnect", "version": "1"},
				"capabilities":    map[string]any{"tools": map[string]any{}},
			}
		case "tools/list":
			response["result"] = map[string]any{"tools": []any{}}
		default:
			response["error"] = map[string]any{"code": -32601, "message": "Method not found"}
		}
		body, _ := json.Marshal(response)
		event := acknowledgedSSEEvent{payload: "event: message\ndata: " + string(body) + "\n\n", done: make(chan struct{})}
		select {
		case events <- event:
		case <-r.Context().Done():
			return
		}
		select {
		case <-event.done:
		case <-r.Context().Done():
			return
		}
		if generation == 1 && request.Method == "tools/list" {
			closeFirst.Do(func() { close(firstClosed) })
		}
		w.WriteHeader(http.StatusAccepted)
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	transport, err := newSSETransport(t.Context(), Spec{Name: "disconnect", Type: "sse", URL: server.URL + "/sse"})
	if err != nil {
		t.Fatal(err)
	}
	transport.reconnectDelays = []time.Duration{time.Millisecond}
	defer transport.close()

	if _, err := transport.call(t.Context(), "tools/list", map[string]any{}); err != nil {
		t.Fatalf("first tools/list: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if _, err := transport.call(ctx, "tools/list", map[string]any{}); err != nil {
		t.Fatalf("tools/list after SSE EOF: %v", err)
	}
	if got := connections.Load(); got != 2 {
		t.Fatalf("SSE connections = %d, want initial + one replacement", got)
	}
}
