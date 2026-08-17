package anthropic

import (
	"context"
	"testing"

	"reasonix/internal/provider"
)

func TestBuildRequestDeepSeekMissingReasoningFallbackDisablesThinkingAndKeepsToolLoop(t *testing.T) {
	c := &client{model: "deepseek-v4-flash", deepseek: true, thinking: "enabled", effort: "high"}
	if !provider.SupportsMissingReasoningFallback(c) {
		t.Fatal("thinking-enabled DeepSeek Anthropic must declare its request-local fallback")
	}
	ctx := provider.WithMissingReasoningFallback(context.Background())
	r := c.buildRequest(ctx, provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "do it"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "t1", Name: "echo", Arguments: `{"text":"hi"}`}}},
		{Role: provider.RoleTool, ToolCallID: "t1", Content: "hi"},
	}})
	if r.Thinking == nil || r.Thinking.Type != "disabled" || r.OutputConfig != nil {
		t.Fatalf("fallback thinking = %+v / %+v, want disabled/no output_config", r.Thinking, r.OutputConfig)
	}
	if len(r.Messages) != 3 || len(r.Messages[1].Content) != 1 || r.Messages[1].Content[0].Type != "tool_use" ||
		len(r.Messages[2].Content) != 1 || r.Messages[2].Content[0].Type != "tool_result" {
		t.Fatalf("fallback tool loop = %+v, want preserved tool_use/tool_result without fabricated thinking", r.Messages)
	}

	native := &client{model: "claude-opus-4-8", nativeAnthropic: true, thinking: "adaptive"}
	if provider.SupportsMissingReasoningFallback(native) {
		t.Fatal("native Anthropic must not declare a mode-switch fallback")
	}
}
