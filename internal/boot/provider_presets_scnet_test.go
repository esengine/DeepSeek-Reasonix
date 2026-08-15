package boot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/provider"
)

func TestNewProviderBuildsSCNetAnthropicRequestContract(t *testing.T) {
	var gotReq map[string]any
	var gotAuth, gotAPIKey, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("x-api-key")
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":1,"output_tokens":0}}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}

event: message_stop
data: {"type":"message_stop"}

`))
	}))
	defer srv.Close()

	preset, ok := config.CuratedProviderPreset("scnet-anthropic")
	if !ok || len(preset.Entries) != 1 {
		t.Fatalf("SCNet Anthropic preset = %+v", preset)
	}
	entry := preset.Entries[0]
	entry.BaseURL = srv.URL
	entry.Model = entry.DefaultModel()
	t.Setenv(entry.APIKeyEnv, "scnet-test-key")
	entry.ResolveAPIKeyFromProcessEnvForProbe()
	p, err := NewProvider(&entry)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}

	if gotPath != "/v1/messages" {
		t.Fatalf("request path = %q, want /v1/messages", gotPath)
	}
	if gotAuth != "Bearer scnet-test-key" {
		t.Fatalf("Authorization = %q, want Bearer scnet-test-key", gotAuth)
	}
	if gotAPIKey != "" {
		t.Fatalf("x-api-key = %q, want omitted", gotAPIKey)
	}
	if _, ok := gotReq["thinking"]; ok {
		t.Fatalf("SCNet MiniMax request must omit thinking: %+v", gotReq)
	}
}
