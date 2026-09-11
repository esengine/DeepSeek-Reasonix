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

// TestRealOpenCodeGoDeepSeekAnthropicWebSearch exercises Reasonix's complete
// Messages serialization and server-side web search parser against the OpenCode
// Go DeepSeek Flash route. The key stays process-local; ordinary CI never runs it.
func TestRealOpenCodeGoDeepSeekAnthropicWebSearch(t *testing.T) {
	key := os.Getenv("OPENCODE_GO_API_KEY")
	if key == "" {
		t.Skip("OPENCODE_GO_API_KEY not set — skipping live probe")
	}

	p, err := New(provider.Config{
		Name:    "opencode-go-deepseek-anthropic",
		BaseURL: "https://opencode.ai/zen/go",
		Model:   "deepseek-v4-flash",
		APIKey:  key,
		Extra: map[string]any{
			"api_key_env":        "OPENCODE_GO_API_KEY",
			"reasoning_protocol": "deepseek",
			"thinking":           "adaptive",
			"effort":             "high",
			"web_search":         true,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	turn := collectLiveDeepSeekTurn(t, p, provider.Request{Messages: []provider.Message{{
		Role: provider.RoleUser, Content: "Search the web for the OpenCode Go documentation and reply with one source URL.",
	}}, MaxTokens: 768})
	if strings.TrimSpace(turn.text) == "" {
		t.Fatalf("OpenCode Go Anthropic web_search returned no assistant text; reasoning_len=%d", len(turn.reasoning))
	}
	t.Logf("opencode-go-deepseek-anthropic web_search: text=%d reasoning=%d prompt=%d", len(turn.text), len(turn.reasoning), turn.promptTokens)
}

// TestRealOpenCodeGoDeepSeekAnthropicToolLoop verifies that the gateway accepts
// an assistant tool call and the corresponding tool result on the next request.
func TestRealOpenCodeGoDeepSeekAnthropicToolLoop(t *testing.T) {
	key := os.Getenv("OPENCODE_GO_API_KEY")
	if key == "" {
		t.Skip("OPENCODE_GO_API_KEY not set — skipping live probe")
	}
	p, err := New(provider.Config{
		Name: "opencode-go-deepseek-anthropic", BaseURL: "https://opencode.ai/zen/go", Model: "deepseek-v4-flash", APIKey: key,
		Extra: map[string]any{
			"api_key_env": "OPENCODE_GO_API_KEY", "reasoning_protocol": "deepseek",
			"thinking": "adaptive", "effort": "high",
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tools := []provider.ToolSchema{{
		Name: "get_marker", Description: "Return a fixed integration-test marker. Always call this tool when the user asks for the marker.",
		Parameters: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	}}
	messages := []provider.Message{
		{Role: provider.RoleSystem, Content: "You are a concise tool-using assistant. Call the requested tool before answering."},
		{Role: provider.RoleUser, Content: "Call get_marker, then report its result."},
	}
	first := collectLiveDeepSeekTurn(t, p, provider.Request{Messages: messages, Tools: tools, MaxTokens: 512})
	if len(first.calls) == 0 {
		t.Fatalf("OpenCode Go Anthropic returned no tool call; text_len=%d reasoning_len=%d", len(first.text), len(first.reasoning))
	}
	messages = append(messages,
		provider.Message{Role: provider.RoleAssistant, Content: first.text, ReasoningContent: first.reasoning, ReasoningSignature: first.signature, ToolCalls: first.calls},
		provider.Message{Role: provider.RoleTool, ToolCallID: first.calls[0].ID, Name: first.calls[0].Name, Content: "protocol-round-trip-ok"},
	)
	second := collectLiveDeepSeekTurn(t, p, provider.Request{Messages: messages, Tools: tools, MaxTokens: 512})
	if strings.TrimSpace(second.text) == "" {
		t.Fatalf("OpenCode Go Anthropic tool follow-up returned no text; reasoning_len=%d calls=%d", len(second.reasoning), len(second.calls))
	}
	t.Logf("opencode-go-deepseek-anthropic tool loop: calls=%d reasoning=%d signature=%d second_text=%d", len(first.calls), len(first.reasoning), len(first.signature), len(second.text))
}

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
			Role:               provider.RoleAssistant,
			Content:            first.text,
			ReasoningContent:   first.reasoning,
			ReasoningSignature: first.signature,
			ToolCalls:          first.calls,
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

// TestRealDeepSeekAnthropicProjectsMissingThinkingHistory reproduces the
// malformed persisted-history shape behind DeepSeek's
// "content[].thinking must be passed back" HTTP 400. The provider boundary
// must project the unreplayable tool activity away before sending the request,
// while retaining the surrounding visible conversation.
func TestRealDeepSeekAnthropicProjectsMissingThinkingHistory(t *testing.T) {
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

	turn := collectLiveDeepSeekTurn(t, p, provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "Inspect the marker using the tool."},
			{
				Role: provider.RoleAssistant, Content: "I inspected the marker.",
				ToolCalls: []provider.ToolCall{{ID: "legacy-call", Name: "get_marker", Arguments: `{}`}},
			},
			{Role: provider.RoleTool, ToolCallID: "legacy-call", Name: "get_marker", Content: "legacy-result"},
			{Role: provider.RoleUser, Content: "Reply with the single word: recovered."},
		},
		MaxTokens: 256,
	})
	if strings.TrimSpace(turn.text) == "" {
		t.Fatalf("projected missing-thinking history returned no assistant text")
	}
	t.Logf("missing-thinking projection: text=%d reasoning=%d prompt=%d", len(turn.text), len(turn.reasoning), turn.promptTokens)
}

// TestRealDeepSeekAnthropicWebSearch verifies that the official Anthropic
// compatibility endpoint accepts the server-side web_search tool and returns a
// normal assistant completion. It is intentionally separate from the tool-loop
// test so a provider-side search regression is visible on its own.
func TestRealDeepSeekAnthropicWebSearch(t *testing.T) {
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
			"web_search":  true,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	turn := collectLiveDeepSeekTurn(t, p, provider.Request{
		Messages: []provider.Message{{
			Role:    provider.RoleUser,
			Content: "Search the web for the latest DeepSeek API documentation update and reply with one source URL.",
		}},
		MaxTokens: 256,
	})
	if strings.TrimSpace(turn.text) == "" {
		t.Fatalf("web_search returned no assistant text (reasoning=%d)", len(turn.reasoning))
	}
	if strings.TrimSpace(turn.reasoning) == "" || len(turn.searches) == 0 {
		t.Fatalf("web_search did not return replayable reasoning/search blocks: reasoning=%d searches=%d", len(turn.reasoning), len(turn.searches))
	}
	continued := collectLiveDeepSeekTurn(t, p, provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "Search the web for the latest DeepSeek API documentation update and reply with one source URL."},
		{Role: provider.RoleAssistant, Content: turn.text, ReasoningContent: turn.reasoning, ServerSearch: turn.searches},
		{Role: provider.RoleUser, Content: "Without searching again, reply with the hostname of that source."},
	}, MaxTokens: 256})
	if strings.TrimSpace(continued.text) == "" {
		t.Fatalf("web_search continuation returned no assistant text (reasoning=%d searches=%d)", len(continued.reasoning), len(continued.searches))
	}
	t.Logf("web_search replay: text=%d reasoning=%d searches=%d prompt=%d cache_hit=%d continuation_text=%d",
		len(turn.text), len(turn.reasoning), len(turn.searches), turn.promptTokens, turn.cacheHitTokens, len(continued.text))
}

// TestRealDeepSeekAnthropicIgnoresImages confirms the text-only official
// endpoint still completes when stale vision metadata accompanies a user turn.
// The provider-boundary filter is what prevents the historical image_url 400.
func TestRealDeepSeekAnthropicIgnoresImages(t *testing.T) {
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
			"vision":      true,
			"thinking":    "disabled",
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	turn := collectLiveDeepSeekTurn(t, p, provider.Request{Messages: []provider.Message{{
		Role:    provider.RoleUser,
		Content: "Reply with the single word: ok.",
		Images:  []string{"data:image/png;base64,AAAA"},
	}}, MaxTokens: 64})
	if strings.TrimSpace(turn.text) == "" {
		t.Fatalf("text-only image-filter smoke returned no assistant text")
	}
	t.Logf("image-filter: text=%d prompt=%d cache_hit=%d", len(turn.text), turn.promptTokens, turn.cacheHitTokens)
}

type liveDeepSeekTurn struct {
	text, reasoning, signature   string
	calls                        []provider.ToolCall
	searches                     []provider.ServerSearchCall
	promptTokens, cacheHitTokens int
}

func collectLiveDeepSeekTurn(t *testing.T, p provider.Provider, req provider.Request) liveDeepSeekTurn {
	return collectLiveDeepSeekTurnContext(t, context.Background(), p, req)
}

func collectLiveDeepSeekTurnContext(t *testing.T, parent context.Context, p provider.Provider, req provider.Request) liveDeepSeekTurn {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
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
			if chunk.Signature != "" {
				out.signature = chunk.Signature
			}
		case provider.ChunkToolCall:
			if chunk.ToolCall != nil {
				out.calls = append(out.calls, *chunk.ToolCall)
			}
		case provider.ChunkServerSearch:
			if chunk.ServerSearch != nil {
				out.searches = provider.MergeServerSearch(out.searches, *chunk.ServerSearch)
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
