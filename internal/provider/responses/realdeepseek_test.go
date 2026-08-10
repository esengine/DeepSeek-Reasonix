//go:build live

package responses

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"reasonix/internal/provider"
)

// TestRealOpenCodeGoDeepSeekResponsesWebSearch exercises Reasonix's stateless
// Responses request, server-side web search, and replay-item capture against the
// OpenCode Go DeepSeek Flash route.
func TestRealOpenCodeGoDeepSeekResponsesWebSearch(t *testing.T) {
	key := os.Getenv("OPENCODE_GO_API_KEY")
	if key == "" {
		t.Skip("OPENCODE_GO_API_KEY not set — skipping live probe")
	}

	p := New(Config{
		Name:            "opencode-go-deepseek-responses",
		BaseURL:         "https://opencode.ai/zen/go/v1",
		Model:           "deepseek-v4-flash",
		APIKey:          key,
		KeyEnv:          "OPENCODE_GO_API_KEY",
		Effort:          "high",
		Mode:            "stateless",
		WebSearch:       true,
		MaxOutputTokens: 768,
	})
	messages := []provider.Message{{
		Role: provider.RoleUser, Content: "Search the web for the OpenCode Go documentation and reply with one source URL.",
	}}
	first := collectLiveOpenCodeGoResponsesTurn(t, p, provider.Request{Messages: messages, MaxTokens: 768})
	if strings.TrimSpace(first.text) == "" {
		t.Fatal("OpenCode Go Responses web_search returned no assistant text")
	}
	if len(first.items) == 0 {
		t.Fatal("OpenCode Go Responses returned no completed web_search_call replay item")
	}
	messages = append(messages,
		provider.Message{Role: provider.RoleAssistant, Content: first.text, ResponsesItems: first.items},
		provider.Message{Role: provider.RoleUser, Content: "Using the previous search result, reply with only the source domain."},
	)
	second := collectLiveOpenCodeGoResponsesTurn(t, p, provider.Request{Messages: messages, MaxTokens: 256})
	if strings.TrimSpace(second.text) == "" {
		t.Fatal("OpenCode Go Responses stateless replay returned no follow-up text")
	}
	t.Logf("opencode-go-deepseek-responses: first_text=%d replay_items=%d second_text=%d", len(first.text), len(first.items), len(second.text))
}

type liveOpenCodeGoResponsesTurn struct {
	text  string
	items []json.RawMessage
}

func collectLiveOpenCodeGoResponsesTurn(t *testing.T, p provider.Provider, req provider.Request) liveOpenCodeGoResponsesTurn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	stream, err := p.Stream(ctx, req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var out liveOpenCodeGoResponsesTurn
	var text strings.Builder
	for chunk := range stream {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
		case provider.ChunkResponsesItem:
			out.items = append(out.items, append(json.RawMessage(nil), chunk.ResponsesItem...))
		case provider.ChunkError:
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
	out.text = text.String()
	return out
}

// TestRealDeepSeekResponsesWebSearch exercises the official stateless
// Responses endpoint with its provider-executed web_search tool. It is
// credential-gated and build-tagged so ordinary CI remains deterministic.
func TestRealDeepSeekResponsesWebSearch(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set — skipping live probe")
	}

	p := New(Config{
		Name:            "deepseek-responses",
		BaseURL:         "https://api.deepseek.com",
		Model:           "deepseek-v4-flash",
		APIKey:          key,
		Effort:          "disabled",
		WebSearch:       true,
		MaxOutputTokens: 256,
		KeyEnv:          "DEEPSEEK_API_KEY",
	})
	chunks := collect(t, p, provider.Request{Messages: []provider.Message{{
		Role:    provider.RoleUser,
		Content: "Search the web for the latest DeepSeek API documentation update and reply with one source URL.",
	}}, MaxTokens: 256})
	var text strings.Builder
	for _, chunk := range chunks {
		if chunk.Type == provider.ChunkText {
			text.WriteString(chunk.Text)
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		t.Fatalf("web_search returned no assistant text")
	}
	t.Logf("web_search: text=%d chunks=%d", len(text.String()), len(chunks))
}
