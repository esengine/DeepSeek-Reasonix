package boot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	_ "reasonix/internal/model/anthropic"
	_ "reasonix/internal/model/openai"
)

func TestMiniMaxPresetsReachHTTP(t *testing.T) {
	for _, id := range []string{"minimax-cn-api", "minimax-global-api", "minimax-cn-anthropic", "minimax-global-anthropic"} {
		for _, model := range []string{"MiniMax-M3.1-Flash", "MiniMax-M3"} {
			for _, effort := range []string{"", "low", "medium", "high", "xhigh", "max", "adaptive", "disabled"} {
				if model == "MiniMax-M3" && effort != "" && effort != "adaptive" && effort != "disabled" {
					continue
				}
				if model == "MiniMax-M3.1-Flash" && (effort == "adaptive" || effort == "disabled") {
					continue
				}
				t.Run(id+"/"+model+"/"+effort, func(t *testing.T) {
					preset, ok := config.CuratedProviderPreset(id)
					if !ok {
						t.Fatal("missing preset")
					}
					cfg := config.Config{Providers: preset.Entries}
					entry, ok := cfg.ResolveModel(id + "/" + model)
					if !ok {
						t.Fatalf("model %s is absent", model)
					}
					entry.Effort = effort
					wire := miniMaxHTTPBody(t, entry)
					if wire["model"] != model || wire["service_tier"] != nil {
						t.Fatalf("unexpected model/tier: %v", wire)
					}
					thinking, _ := wire["thinking"].(map[string]any)
					wantThinking := "adaptive"
					if model == "MiniMax-M3" && (effort == "disabled" || (entry.Kind == "anthropic" && effort == "")) {
						wantThinking = "disabled"
					}
					if thinking["type"] != wantThinking || thinking["display"] != nil {
						t.Fatalf("thinking = %v, want %s without display", thinking, wantThinking)
					}
					gotEffort := wire["reasoning_effort"]
					if entry.Kind == "anthropic" {
						output, _ := wire["output_config"].(map[string]any)
						gotEffort = output["effort"]
					}
					if model == "MiniMax-M3.1-Flash" {
						want := effort
						if want == "" {
							want = "max"
						}
						if gotEffort != want {
							t.Fatalf("effort = %v, want %s", gotEffort, want)
						}
					} else if gotEffort != nil {
						t.Fatalf("M3 received depth: %v", gotEffort)
					}
					body, _ := json.Marshal(wire)
					for _, marker := range []string{"reasoning-marker", "tool-marker", "image", "aGVsbG8="} {
						if !strings.Contains(string(body), marker) {
							t.Errorf("request lost %s", marker)
						}
					}
				})
			}
		}
	}
}

func miniMaxHTTPBody(t *testing.T, entry *config.ProviderEntry) map[string]any {
	t.Helper()
	captured := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		captured <- body
		w.Header().Set("Content-Type", "text/event-stream")
		if entry.Kind == "anthropic" {
			_, _ = io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		} else {
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
		}
	}))
	defer srv.Close()
	keyed := entry.WithAPIKeyForProbe("test-key")
	entry = &keyed
	entry.ChatURL = srv.URL + "/v1/chat/completions"
	entry.RequestURL = srv.URL + "/v1/messages"
	p, err := NewProviderWithProxy(entry, netclient.ProxySpec{})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "describe", Images: []string{"data:image/png;base64,aGVsbG8="}},
		{Role: provider.RoleAssistant, Content: "checking", ReasoningContent: "reasoning-marker", ToolCalls: []provider.ToolCall{{ID: "tool-marker", Name: "inspect", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "tool-marker", Content: "done"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for chunk := range stream {
		if chunk.Type == provider.ChunkError {
			t.Fatal(chunk.Err)
		}
	}
	select {
	case body := <-captured:
		return body
	default:
		t.Fatal("request never reached HTTP")
		return nil
	}
}

func TestMiniMaxOpenRouterFlashReachesHTTP(t *testing.T) {
	preset, _ := config.CuratedProviderPreset("openrouter")
	cfg := config.Config{Providers: preset.Entries}
	for _, effort := range []string{"", "low", "medium", "high", "xhigh", "max"} {
		entry, ok := cfg.ResolveModel("openrouter/minimax/minimax-m3.1-flash:free")
		if !ok {
			t.Fatal("gateway Flash did not resolve")
		}
		entry.Effort = effort
		wire := miniMaxHTTPBody(t, entry)
		if effort == "" {
			effort = "max"
		}
		if wire["model"] != "minimax/minimax-m3.1-flash:free" || wire["reasoning_effort"] != effort {
			t.Fatalf("gateway request = %v", wire)
		}
	}
}

func TestMiniMaxFlashRejectsDisablingThinking(t *testing.T) {
	for _, kind := range []string{"openai", "anthropic"} {
		for _, level := range []string{"none", "disabled", "off", "adaptive"} {
			_, err := provider.New(kind, provider.Config{BaseURL: "https://api.minimax.io", Model: "MiniMax-M3.1-Flash", APIKey: "test", Extra: map[string]any{"effort": level}})
			if err == nil {
				t.Errorf("%s accepted invalid effort %s", kind, level)
			}
		}
		_, err := provider.New(kind, provider.Config{BaseURL: "https://api.minimax.io", Model: "MiniMax-M3.1-Flash", APIKey: "test", Extra: map[string]any{"thinking": "disabled"}})
		if err == nil {
			t.Errorf("%s accepted disabled thinking", kind)
		}
	}
}

func TestMiniMaxSavedAndCustomModelsReachHTTPUnchanged(t *testing.T) {
	for _, kind := range []string{"openai", "anthropic"} {
		for _, model := range []string{"MiniMax-M2.7", "custom-model"} {
			t.Run(kind+"/"+model, func(t *testing.T) {
				entry := &config.ProviderEntry{Name: "saved", Kind: kind, BaseURL: "https://api.minimax.io/anthropic", Model: model}
				wire := miniMaxHTTPBody(t, entry)
				if wire["model"] != model {
					t.Fatalf("saved model changed: %v", wire["model"])
				}
			})
		}
	}
}
