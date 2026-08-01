//go:build live

package anthropic

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"reasonix/internal/provider"
)

// TestRealDeepSeekAnthropicToolLoop exercises the official Messages endpoint's
// unsigned thinking replay contract. It is build-tagged and credential-gated so
// ordinary CI remains deterministic and free of live API cost.
func TestRealDeepSeekAnthropicToolLoop(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set — skipping live probe")
	}

	p, err := New(provider.Config{
		Name:    "deepseek-anthropic",
		BaseURL: "https://api.deepseek.com/anthropic",
		Model:   "deepseek-v4-flash",
		APIKey:  key,
		Extra: map[string]any{
			"api_key_env": "DEEPSEEK_API_KEY",
			"thinking":    "enabled",
			"effort":      "high",
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tools := []provider.ToolSchema{{
		Name:        "get_marker",
		Description: "Return a fixed integration-test marker. Call this tool when the user asks for the marker.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	}}
	messages := []provider.Message{
		{Role: provider.RoleSystem, Content: "You are a concise tool-using assistant."},
		{Role: provider.RoleUser, Content: "Use get_marker to obtain the integration-test marker, then tell me the result."},
	}

	first := collectLiveDeepSeekTurn(t, p, provider.Request{Messages: messages, Tools: tools, MaxTokens: 512})
	if len(first.calls) == 0 {
		t.Fatalf("first turn returned no tool call; text=%q reasoning_len=%d", first.text, len(first.reasoning))
	}
	if strings.TrimSpace(first.reasoning) == "" {
		t.Fatal("first tool-call turn returned no reasoning to replay")
	}

	messages = append(messages,
		provider.Message{
			Role:             provider.RoleAssistant,
			Content:          first.text,
			ReasoningContent: first.reasoning,
			ToolCalls:        first.calls,
		},
		provider.Message{
			Role:       provider.RoleTool,
			ToolCallID: first.calls[0].ID,
			Name:       first.calls[0].Name,
			Content:    "protocol-round-trip-ok",
		},
	)
	second := collectLiveDeepSeekTurn(t, p, provider.Request{Messages: messages, Tools: tools, MaxTokens: 512})
	if strings.TrimSpace(second.text) == "" {
		t.Fatalf("second turn returned no visible answer; reasoning_len=%d calls=%d", len(second.reasoning), len(second.calls))
	}
	t.Logf("first: reasoning=%d calls=%d prompt=%d cache_hit=%d; second: text=%d reasoning=%d prompt=%d cache_hit=%d",
		len(first.reasoning), len(first.calls), first.promptTokens, first.cacheHitTokens,
		len(second.text), len(second.reasoning), second.promptTokens, second.cacheHitTokens)
}

type liveDeepSeekTurn struct {
	text, reasoning              string
	calls                        []provider.ToolCall
	promptTokens, cacheHitTokens int
}

func collectLiveDeepSeekTurn(t *testing.T, p provider.Provider, req provider.Request) liveDeepSeekTurn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ch, err := p.Stream(ctx, req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var out liveDeepSeekTurn
	var text, reasoning strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
		case provider.ChunkReasoning:
			reasoning.WriteString(chunk.Text)
		case provider.ChunkToolCall:
			if chunk.ToolCall != nil {
				out.calls = append(out.calls, *chunk.ToolCall)
			}
		case provider.ChunkUsage:
			if chunk.Usage != nil {
				out.promptTokens = chunk.Usage.PromptTokens
				out.cacheHitTokens = chunk.Usage.CacheHitTokens
			}
		case provider.ChunkError:
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
	out.text = text.String()
	out.reasoning = reasoning.String()
	return out
}
