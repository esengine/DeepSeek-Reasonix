//go:build live

package openai

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"reasonix/internal/provider"
)

func TestRealLongCatToolReasoningReplay(t *testing.T) {
	runLiveCompatibleToolReplay(t, liveCompatibleConfig{
		env: "LONGCAT_API_KEY", name: "longcat", baseURL: "https://api.longcat.chat/openai/v1", model: "LongCat-2.0",
		extra: map[string]any{"thinking": "enabled"},
	})
}

func TestRealZhipuCodingPlanToolReasoningReplay(t *testing.T) {
	runLiveCompatibleToolReplay(t, liveCompatibleConfig{
		env: "ZHIPU_CODING_API_KEY", name: "zhipu-coding", baseURL: "https://api.z.ai/api/coding/paas/v4", model: "glm-5.1",
		extra: map[string]any{"thinking": "enabled"},
	})
}

func TestRealOpenCodeGoDeepSeekToolReasoningReplay(t *testing.T) {
	runLiveCompatibleToolReplay(t, liveCompatibleConfig{
		env: "OPENCODE_GO_API_KEY", name: "opencode-go", baseURL: "https://opencode.ai/zen/go/v1", model: "deepseek-v4-flash",
		extra: map[string]any{"thinking": "enabled", "effort": "high"},
	})
}

type liveCompatibleConfig struct {
	env, name, baseURL, model string
	extra                     map[string]any
}

func runLiveCompatibleToolReplay(t *testing.T, cfg liveCompatibleConfig) {
	t.Helper()
	key := os.Getenv(cfg.env)
	if key == "" {
		t.Skipf("%s not set — skipping live probe", cfg.env)
	}
	cfg.extra["api_key_env"] = cfg.env
	p, err := New(provider.Config{Name: cfg.name, BaseURL: cfg.baseURL, Model: cfg.model, APIKey: key, Extra: cfg.extra})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tools := []provider.ToolSchema{{
		Name: "get_marker", Description: "Return a fixed integration-test marker. Always call this tool when asked for the marker.",
		Parameters: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	}}
	messages := []provider.Message{
		{Role: provider.RoleSystem, Content: "You are a concise tool-using assistant. Call the requested tool before answering."},
		{Role: provider.RoleUser, Content: "Call get_marker, then report its result."},
	}
	first := collectLiveCompatibleTurn(t, p, provider.Request{Messages: messages, Tools: tools, MaxTokens: 512})
	if len(first.calls) == 0 {
		t.Fatalf("first turn returned no tool call; text=%d reasoning=%d", len(first.text), len(first.reasoning))
	}
	messages = append(messages,
		provider.Message{Role: provider.RoleAssistant, Content: first.text, ReasoningContent: first.reasoning, ToolCalls: first.calls},
		provider.Message{Role: provider.RoleTool, ToolCallID: first.calls[0].ID, Name: first.calls[0].Name, Content: "protocol-round-trip-ok"},
	)
	second := collectLiveCompatibleTurn(t, p, provider.Request{Messages: messages, Tools: tools, MaxTokens: 512})
	if strings.TrimSpace(second.text) == "" {
		t.Fatalf("tool replay returned no visible answer; reasoning=%d calls=%d", len(second.reasoning), len(second.calls))
	}
	if first.promptTokens == 0 || second.promptTokens == 0 {
		t.Fatalf("usage missing: first_prompt=%d second_prompt=%d", first.promptTokens, second.promptTokens)
	}
	t.Logf("%s tool replay: reasoning=%d calls=%d first_prompt=%d second_text=%d second_prompt=%d",
		cfg.name, len(first.reasoning), len(first.calls), first.promptTokens, len(second.text), second.promptTokens)
}

type liveCompatibleTurn struct {
	text, reasoning string
	calls           []provider.ToolCall
	promptTokens    int
}

func collectLiveCompatibleTurn(t *testing.T, p provider.Provider, req provider.Request) liveCompatibleTurn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ch, err := p.Stream(ctx, req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var out liveCompatibleTurn
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
			}
		case provider.ChunkError:
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
	out.text, out.reasoning = text.String(), reasoning.String()
	return out
}
