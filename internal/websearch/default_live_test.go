//go:build live

package websearch_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/netclient"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
	"reasonix/internal/websearch"
)

// Explicitly opt in to two bounded main-model calls and one independent search.
// Only counts are logged; no credentials or conversation content are written.
func TestLiveDefaultDeepSeekSearchRoundTrip(t *testing.T) {
	if os.Getenv("REASONIX_LIVE_WEB_SEARCH") != "1" {
		t.Skip("set REASONIX_LIVE_WEB_SEARCH=1")
	}
	cfg := config.Default()
	entry, ok := cfg.ResolveModel(cfg.DefaultModel)
	if !ok {
		t.Fatal("default provider did not resolve")
	}
	entry.ResolveAPIKeyFromProcessEnvForProbe()
	if !entry.Configured() {
		t.Skip("official account not configured")
	}
	if entry.Kind != "openai" {
		t.Fatalf("default kind = %s", entry.Kind)
	}
	proxy := netclient.ProxySpec{Mode: netclient.ModeAuto}
	p, err := boot.NewProviderWithProxy(entry, proxy)
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := p.(interface{ CloseIdleConnections() }); ok {
		defer c.CloseIdleConnections()
	}
	reg := tool.NewRegistry()
	route := cfg.ResolveWebSearchProvider(entry)
	if route == nil {
		t.Fatal("no independent search route")
	}
	reg.Add(&websearch.Tool{Factory: func() (provider.Provider, error) {
		return provider.New(route.Kind, provider.Config{Name: route.Name, BaseURL: route.BaseURL, Model: route.Model, APIKey: route.APIKey(), Extra: map[string]any{"web_search": true, "reject_redirects": true, "thinking": route.Thinking, "effort": route.Effort}})
	}})
	search, ok := reg.Get("web_search")
	if !ok {
		t.Fatal("missing independent search")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	collect := func(req provider.Request) provider.Message {
		t.Helper()
		ch, err := p.Stream(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		m := provider.Message{Role: provider.RoleAssistant}
		done := false
		for c := range ch {
			switch c.Type {
			case provider.ChunkText:
				m.Content += c.Text
			case provider.ChunkReasoning:
				m.ReasoningContent += c.Text
			case provider.ChunkToolCall:
				if c.ToolCall != nil {
					m.ToolCalls = append(m.ToolCalls, *c.ToolCall)
				}
			case provider.ChunkError:
				t.Fatalf("main request failed: %v", c.Err)
			case provider.ChunkDone:
				done = true
			}
		}
		if !done {
			t.Fatal("main request interrupted")
		}
		return m
	}
	messages := []provider.Message{{Role: provider.RoleUser, Content: "Call web_search exactly once for the official DeepSeek thinking-mode API documentation, then summarize its reasoning passback requirement in one sentence with a source link."}}
	first := collect(provider.Request{Messages: messages, Tools: reg.Schemas(), MaxTokens: 2048})
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].Name != "web_search" {
		t.Fatalf("expected one search call, got %d", len(first.ToolCalls))
	}
	call := first.ToolCalls[0]
	output, err := search.Execute(ctx, json.RawMessage(call.Arguments))
	if err != nil {
		t.Fatal(err)
	}
	sources := provider.ParseServerSearchOutput(output)
	if len(sources) == 0 {
		t.Fatal("no structured search sources")
	}
	messages = append(messages, first, provider.Message{Role: provider.RoleTool, ToolCallID: call.ID, Name: call.Name, Content: output})
	last := collect(provider.Request{Messages: messages, Tools: reg.Schemas(), MaxTokens: 2048})
	if last.Content == "" || len(last.ToolCalls) > 0 {
		t.Fatal("main model did not finish after search")
	}
	t.Logf("main_protocol=%s sources=%d first_reasoning_bytes=%d final_bytes=%d", entry.Kind, len(sources), len(first.ReasoningContent), len(last.Content))
}
