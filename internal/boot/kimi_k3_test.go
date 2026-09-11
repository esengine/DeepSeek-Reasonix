package boot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/provider"
)

func TestNewProviderAppliesExplicitKimiK3RequestContractToCustomGateway(t *testing.T) {
	var gotReq map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"thinking\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	p, err := NewProvider(&config.ProviderEntry{
		Name: "custom-kimi-gateway", Kind: "openai", BaseURL: srv.URL, Model: "kimi-k3",
		ReasoningProtocol: config.ReasoningProtocolKimiK3,
		SupportedEfforts:  []string{"medium", "ultra"}, DefaultEffort: "ultra",
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if !provider.RequiresReasoningRoundTrip(p) {
		t.Fatal("custom Kimi K3 provider must advertise reasoning round-trip")
	}
	ch, err := p.Stream(context.Background(), provider.Request{
		Messages:    []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		Temperature: provider.TemperaturePtr(0.3), MaxTokens: 2048,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var reasoning strings.Builder
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		if chunk.Type == provider.ChunkReasoning {
			reasoning.WriteString(chunk.Text)
		}
	}
	if reasoning.String() != "thinking" {
		t.Fatalf("reasoning stream = %q, want thinking", reasoning.String())
	}
	if gotReq["reasoning_effort"] != "max" || gotReq["max_completion_tokens"] != float64(2048) {
		t.Fatalf("custom Kimi K3 request = %+v, want protocol-default max effort and max_completion_tokens", gotReq)
	}
	for _, field := range []string{"temperature", "max_tokens"} {
		if _, ok := gotReq[field]; ok {
			t.Fatalf("custom Kimi K3 request must omit %q: %+v", field, gotReq)
		}
	}
}
