package openai

import (
	"testing"

	"reasonix/internal/provider"
)

func TestDeepSeekReplaysEveryReasoningCarryingAssistantTurn(t *testing.T) {
	p, err := New(provider.Config{
		Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-pro", APIKey: "k",
		Extra: map[string]any{"reasoning_protocol": "deepseek", "thinking": "enabled"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !provider.RequiresAssistantReasoningReplay(p, provider.Message{
		Role: provider.RoleAssistant, Content: "plain", ReasoningContent: "provider reasoning",
	}) {
		t.Fatal("DeepSeek plain assistant reasoning must be replay-required")
	}
	if provider.RequiresAssistantReasoningReplay(p, provider.Message{
		Role: provider.RoleAssistant, Content: "plain",
	}) {
		t.Fatal("DeepSeek plain assistant without reasoning must not invent a replay requirement")
	}
	if !provider.RequiresAssistantReasoningReplay(p, provider.Message{
		Role:      provider.RoleAssistant,
		ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "read_file"}},
	}) {
		t.Fatal("DeepSeek tool turn must remain replay-required when reasoning is missing")
	}

	disabled := p.(*client)
	disabled.thinkingType = "disabled"
	if !provider.RequiresAssistantReasoningReplay(disabled, provider.Message{
		Role: provider.RoleAssistant, Content: "plain", ReasoningContent: "previous reasoning",
	}) {
		t.Fatal("thinking-disabled DeepSeek must still replay stored reasoning")
	}
	if provider.RequiresAssistantReasoningReplay(disabled, provider.Message{
		Role:      provider.RoleAssistant,
		ToolCalls: []provider.ToolCall{{ID: "call_2", Name: "read_file"}},
	}) {
		t.Fatal("thinking-disabled DeepSeek must not require missing tool reasoning")
	}
}

func TestGLMPreservesIssuedReasoningAndAllowsEmptyToolFallback(t *testing.T) {
	p, err := New(provider.Config{
		Name: "glm", BaseURL: "https://open.bigmodel.cn/api/coding/paas/v4", Model: "glm-5.2", APIKey: "k",
		Extra: map[string]any{"reasoning_protocol": "glm"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !provider.RequiresReasoningRoundTrip(p) {
		t.Fatal("thinking-enabled GLM must preserve provider-issued reasoning")
	}
	if !provider.RequiresAssistantReasoningReplay(p, provider.Message{Role: provider.RoleAssistant, ReasoningContent: "provider reasoning"}) {
		t.Fatal("GLM must replay reasoning the provider emitted")
	}
	if provider.RequiresAssistantReasoningReplay(p, provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "read_file"}}}) {
		t.Fatal("GLM tool turns without provider reasoning must remain replayable")
	}
	if !provider.AllowsEmptyReasoningFallback(p) {
		t.Fatal("GLM must accept an empty reasoning_content value when the provider emitted none")
	}
	if provider.RequiresAssistantReasoningReplay(p, provider.Message{Role: provider.RoleAssistant, Content: "plain answer"}) {
		t.Fatal("GLM plain answers without reasoning must remain replayable")
	}
}

func TestGLMSerializesEmptyReasoningContentForToolHistory(t *testing.T) {
	p, err := New(provider.Config{
		Name: "glm", BaseURL: "https://open.bigmodel.cn/api/coding/paas/v4", Model: "glm-5.2", APIKey: "k",
		Extra: map[string]any{"reasoning_protocol": "glm"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := p.(*client).buildRequest(provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "inspect"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "read_file", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "call_1", Name: "read_file", Content: "package main"},
	}})
	if got := req.Messages[1].ReasoningContent; got == nil || *got != "" {
		t.Fatalf("GLM empty tool reasoning_content = %v, want explicit empty string", got)
	}
}
