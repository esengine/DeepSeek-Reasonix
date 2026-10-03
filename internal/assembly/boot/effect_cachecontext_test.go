package boot

// Effect tests for cachecontext at the provider boundary through the real Build
// stack: the id reaches any OpenAI- or Anthropic-compatible provider's request
// body (`user`/`session_id` on the OpenAI wire, metadata.user_id on the other).

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
)

const cacheContextOpenAIStream = "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"

const cacheContextAnthropicStream = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":2}}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}

event: message_stop
data: {"type":"message_stop"}

`

// cacheContextWire runs one turn through Build against a real provider client
// of the given kind pointed at a local test server, and returns the JSON request
// body that reached it.
func cacheContextWire(t *testing.T, kind, stream, cachecontext string) map[string]any {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	var (
		mu   sync.Mutex
		body map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(raw, &got)
		mu.Lock()
		body = got
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, stream)
	}))
	t.Cleanup(srv.Close)

	writeFile(t, dir, "reasonix.toml", `
default_model = "gateway"
cachecontext = "`+cachecontext+`"

[[providers]]
name = "gateway"
kind = "`+kind+`"
base_url = "`+srv.URL+`"
model = "m"
api_key = "test-key"
`)
	approveWorkspace(t, dir)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "reply ok"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if body == nil {
		t.Fatal("no request reached the endpoint")
	}
	return body
}

// TestEffectCacheContextReachesTheAnthropicWire: a project cachecontext reaches
// any Anthropic-compatible provider as metadata.user_id, through the real Build
// stack — the Anthropic wire had no effect test before.
func TestEffectCacheContextReachesTheAnthropicWire(t *testing.T) {
	body := cacheContextWire(t, "anthropic", cacheContextAnthropicStream, "proj-gamma")
	meta, ok := body["metadata"].(map[string]any)
	if !ok || meta["user_id"] != "proj-gamma" {
		t.Fatalf("metadata = %#v, want user_id %q", body["metadata"], "proj-gamma")
	}
}

// TestEffectAutoCacheContextReachesTheRequest: cachecontext = "auto" derives the
// id from the workspace key, and that derived value is what reaches the request
// body as the `user` field.
func TestEffectAutoCacheContextReachesTheRequest(t *testing.T) {
	body := cacheContextWire(t, "openai", cacheContextOpenAIStream, "auto")
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	cfg, err := config.LoadForRoot(dir)
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	want := cfg.EffectiveCacheContext(dir)
	if want == "" {
		t.Fatal("opt-in auto cachecontext is empty")
	}
	if body["user"] != want {
		t.Fatalf("wire user = %#v, want auto %q", body["user"], want)
	}
}

// TestEffectSessionIDReachesOpenAICompatibleWire: the workspace session id
// reaches any OpenAI-compatible provider as the top-level session_id.
func TestEffectSessionIDReachesOpenAICompatibleWire(t *testing.T) {
	body := cacheContextWire(t, "openai", cacheContextOpenAIStream, "proj-gamma")
	if body["session_id"] != "proj-gamma" {
		t.Fatalf("wire session_id = %#v, want %q", body["session_id"], "proj-gamma")
	}
}
