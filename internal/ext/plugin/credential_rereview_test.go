package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRereviewSSEAuxiliaryWarningsAndRPCIdentity(t *testing.T) {
	var sessions sync.Map
	var next atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Basic fixturesecret" {
			t.Error("operational header changed")
		}
		if r.Method == http.MethodGet {
			path := fmt.Sprintf("/messages/%d", next.Add(1))
			events := make(chan []byte, 8)
			sessions.Store(path, events)
			defer sessions.Delete(path)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", path)
			w.(http.Flusher).Flush()
			for {
				select {
				case <-r.Context().Done():
					return
				case body := <-events:
					_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", body)
					w.(http.Flusher).Flush()
				}
			}
		}
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if req.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		response := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		switch req.Method {
		case "initialize":
			response["result"] = map[string]any{"protocolVersion": protocolVersion, "serverInfo": map[string]any{"name": "neutral", "version": "0"}, "capabilities": map[string]any{"tools": map[string]any{}, "prompts": map[string]any{}, "resources": map[string]any{}}}
		case "tools/list":
			response["result"] = map[string]any{"tools": []any{}}
		default:
			response["error"] = map[string]any{"code": -32000, "message": "endpoint https://host/mcp?%74oken=fixturesecret"}
		}
		body, _ := json.Marshal(response)
		value, ok := sessions.Load(r.URL.Path)
		if !ok {
			t.Error("missing session")
			return
		}
		value.(chan []byte) <- body
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	spec := Spec{Name: "neutral", Type: "sse", URL: srv.URL + "/sse", Headers: map[string]string{"Authorization": "Basic fixturesecret"}}
	host, _, err := StartAll(ctx, []Spec{spec})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	for _, fetch := range []func(context.Context, *Client){
		func(ctx context.Context, c *Client) { host.fetchPrompts(ctx, c, nil) },
		func(ctx context.Context, c *Client) { host.fetchResources(ctx, c, nil) },
	} {
		logs.Reset()
		fetch(ctx, host.clients[0])
		if logs.Len() == 0 || strings.Contains(logs.String(), "fixturesecret") {
			t.Errorf("auxiliary warning leaked: %s", logs.String())
		}
	}
	transport, err := newSSETransport(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close()
	_, err = transport.call(ctx, "prompts/list", nil)
	var rpc *rpcError
	if !errors.As(err, &rpc) || rpc.Code != -32000 {
		t.Fatalf("RPC identity lost: %v", err)
	}
	if strings.Contains(err.Error(), "fixturesecret") {
		t.Errorf("RPC diagnostic leaked: %v", err)
	}
	result, err := transport.call(ctx, "tools/list", nil)
	if err != nil || !strings.Contains(string(result), "tools") {
		t.Fatalf("successful result changed: %s %v", result, err)
	}
}
