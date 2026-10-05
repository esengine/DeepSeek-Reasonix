package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// versionServer is a Streamable HTTP server that answers initialize with the
// revision it is told to, and records what the client sent.
type versionServer struct {
	answer string

	mu        sync.Mutex
	headers   map[string]string // method -> MCP-Protocol-Version it arrived with
	deletes   int
	cancelled []float64
}

func (v *versionServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			v.mu.Lock()
			v.deletes++
			v.mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}
		var req struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		v.mu.Lock()
		v.headers[req.Method] = r.Header.Get("MCP-Protocol-Version")
		if req.Method == cancelledMethod {
			var p struct {
				RequestID float64 `json:"requestId"`
			}
			_ = json.Unmarshal(req.Params, &p)
			v.cancelled = append(v.cancelled, p.RequestID)
		}
		v.mu.Unlock()
		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "s-1")
		}
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": v.answer, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{"name": "slow", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result})
	})
}

func startVersionServer(t *testing.T, answer string) (*versionServer, *httptest.Server) {
	t.Helper()
	v := &versionServer{answer: answer, headers: map[string]string{}}
	srv := httptest.NewServer(v.handler())
	t.Cleanup(srv.Close)
	return v, srv
}

// The client offers the newest revision and runs on whichever supported one
// the server picks, stating it on every request after initialize and never on
// initialize itself: a server that gets no header assumes 2025-03-26.
func TestHTTPSessionStatesTheNegotiatedVersionAfterInitialize(t *testing.T) {
	for _, answer := range supportedProtocolVersions {
		v, srv := startVersionServer(t, answer)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		host, tools, err := StartAll(ctx, []Spec{{Name: "v", Type: "http", URL: srv.URL}})
		if err != nil {
			cancel()
			t.Fatalf("%s: StartAll: %v", answer, err)
		}
		host.Close()
		cancel()
		if len(tools) != 1 {
			t.Fatalf("%s: tools = %d", answer, len(tools))
		}
		v.mu.Lock()
		if got := v.headers["initialize"]; got != "" {
			t.Errorf("%s: initialize carried MCP-Protocol-Version %q", answer, got)
		}
		if got := v.headers["tools/list"]; got != answer {
			t.Errorf("%s: tools/list carried %q, want the negotiated %q", answer, got, answer)
		}
		if v.deletes != 1 {
			t.Errorf("%s: close sent %d DELETEs, want the session ended once", answer, v.deletes)
		}
		v.mu.Unlock()
	}
}

// A revision this client cannot speak is refused by identity, not read on as
// if it were one of ours.
func TestHTTPSessionRefusesAnUnsupportedVersion(t *testing.T) {
	_, srv := startVersionServer(t, "2099-01-01")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	host, _, err := StartAll(ctx, []Spec{{Name: "v", Type: "http", URL: srv.URL}})
	if host != nil {
		host.Close()
	}
	if !errors.Is(err, ErrUnsupportedProtocolVersion) {
		t.Fatalf("err = %v, want ErrUnsupportedProtocolVersion", err)
	}
}

// A call the host walks away from is cancelled at the server over Streamable
// HTTP too, as it already was over stdio and SSE.
func TestHTTPCallCancelledByTheHostIsCancelledAtTheServer(t *testing.T) {
	v, srv := startVersionServer(t, protocolVersion)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	host, tools, err := StartAll(ctx, []Spec{{Name: "v", Type: "http", URL: srv.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	callCtx, stop := context.WithTimeout(ctx, 200*time.Millisecond)
	defer stop()
	if _, err := tools[0].Execute(callCtx, json.RawMessage(`{}`)); err == nil {
		t.Fatal("a call that never answered returned no error")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		v.mu.Lock()
		n := len(v.cancelled)
		v.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no notifications/cancelled reached the server")
}

func TestNegotiatedVersionReadsAnOlderServer(t *testing.T) {
	if got, err := negotiatedVersion(""); err != nil || got != "2024-11-05" {
		t.Fatalf("empty reply = %q, %v; want the oldest revision", got, err)
	}
	if got, err := negotiatedVersion("2025-03-26"); err != nil || got != "2025-03-26" {
		t.Fatalf("2025-03-26 = %q, %v", got, err)
	}
}
