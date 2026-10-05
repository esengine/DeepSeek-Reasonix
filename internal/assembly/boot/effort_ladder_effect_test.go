package boot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"

	_ "reasonix/internal/model/responses"
)

// captureReasoningRequest serves one streamed turn and records the request body.
func captureReasoningRequest(t *testing.T, sse string) (*httptest.Server, *map[string]any) {
	t.Helper()
	got := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

const chatCompletionsSSE = "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"

const responsesSSE = "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"status\":\"completed\",\"output\":[]}}\n\n"

// streamOnce builds the provider exactly as a session would — the level set
// through /effort, then the real provider factory — and runs one turn.
func streamOnce(t *testing.T, e *config.ProviderEntry, level string) {
	t.Helper()
	stored, err := config.NormalizeEffort(e, level)
	if err != nil {
		t.Fatalf("NormalizeEffort(%q) on %s: %v", level, e.Model, err)
	}
	e.Effort = stored
	p, err := newProviderForTest(e)
	if err != nil {
		t.Fatalf("building %s at effort %q: %v", e.Model, level, err)
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
}

// Every rung the composer offers for a declared model reaches the Chat
// Completions wire as itself: none of them is refused at build time or clamped
// to a shallower level on the way out.
func TestEveryOfferedRungReachesTheChatCompletionsRequest(t *testing.T) {
	cases := []struct {
		model, baseURL string
		want           []string
	}{
		{"gpt-5.6-sol", "https://api.openai.com/v1", []string{"none", "low", "medium", "high", "xhigh", "max"}},
		{"gpt-5.6", "https://api.openai.com/v1", []string{"none", "low", "medium", "high", "xhigh", "max"}},
		{"qwen3.8-flash", "https://dashscope.aliyuncs.com/compatible-mode/v1", []string{"none", "low", "medium", "xhigh"}},
		{"qwen3.8-max", "https://ws-1.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", []string{"none", "low", "medium", "xhigh"}},
	}
	for _, tc := range cases {
		entry := &config.ProviderEntry{Name: "p", Kind: "openai", BaseURL: tc.baseURL, Model: tc.model}
		offered := config.EffortCapabilityForEntry(entry).Levels
		if len(offered) != len(tc.want)+1 {
			t.Fatalf("%s offers %v, want auto + %v", tc.model, offered, tc.want)
		}
		for _, level := range tc.want {
			srv, got := captureReasoningRequest(t, chatCompletionsSSE)
			e := *entry
			e.RequestURL = srv.URL
			streamOnce(t, &e, level)
			if (*got)["reasoning_effort"] != level {
				t.Fatalf("%s at %q sent reasoning_effort=%v", tc.model, level, (*got)["reasoning_effort"])
			}
		}
	}
}

// A level the model does not take never leaves the host: a relay serving
// gpt-5.6 has not been shown to take xhigh, so it is answered with high.
func TestUndeclaredRungDegradesBeforeTheRequest(t *testing.T) {
	srv, got := captureReasoningRequest(t, chatCompletionsSSE)
	e := &config.ProviderEntry{Name: "relay", Kind: "openai", BaseURL: "https://relay.example.com/v1",
		Model: "gpt-5.6-sol", RequestURL: srv.URL}
	streamOnce(t, e, "xhigh")
	if (*got)["reasoning_effort"] != "high" {
		t.Fatalf("relay xhigh sent reasoning_effort=%v, want high", (*got)["reasoning_effort"])
	}
	srv2, got2 := captureReasoningRequest(t, chatCompletionsSSE)
	q := &config.ProviderEntry{Name: "q", Kind: "openai", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model: "qwen3.8-flash", RequestURL: srv2.URL}
	streamOnce(t, q, "high")
	if (*got2)["reasoning_effort"] != "xhigh" {
		t.Fatalf("qwen3.8 high sent reasoning_effort=%v, want xhigh", (*got2)["reasoning_effort"])
	}
}

// GPT-6 is declared on the Responses wire, where the level rides reasoning.effort.
func TestGPT6RungsReachTheResponsesRequest(t *testing.T) {
	for _, tc := range []struct{ model, level string }{
		{"gpt-6-sol", "none"}, {"gpt-6-sol", "max"}, {"gpt-6-astra", "xhigh"}, {"gpt-5.6-terra", "max"},
	} {
		srv, got := captureReasoningRequest(t, responsesSSE)
		e := &config.ProviderEntry{Name: "oai", Kind: "responses", BaseURL: "https://api.openai.com/v1",
			Model: tc.model, RequestURL: srv.URL}
		streamOnce(t, e, tc.level)
		reasoning, _ := (*got)["reasoning"].(map[string]any)
		if reasoning["effort"] != tc.level {
			t.Fatalf("%s at %q sent reasoning=%v", tc.model, tc.level, (*got)["reasoning"])
		}
	}
	astra := &config.ProviderEntry{Name: "oai", Kind: "responses", BaseURL: "https://api.openai.com/v1", Model: "gpt-6-astra"}
	if _, err := config.NormalizeEffort(astra, "none"); err == nil {
		t.Fatal("gpt-6-astra accepted none, which the endpoint answers with 400")
	}
}
