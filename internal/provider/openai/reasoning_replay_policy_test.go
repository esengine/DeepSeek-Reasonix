package openai

import (
	"testing"

	"reasonix/internal/provider"
)

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
