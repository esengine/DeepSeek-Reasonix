package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// A generic gateway explicitly configured with thinking=enabled inherits the
// DeepSeek reasoning replay contract for every reasoning-carrying turn.
func TestBuildRequestThinkingEnabledGatewayRoundTripsAllReasoning(t *testing.T) {
	p, err := New(provider.Config{
		Name: "custom", BaseURL: "https://gateway.example/v1", Model: "ds4-flash", APIKey: "k",
		Extra: map[string]any{"thinking": "enabled"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !provider.RequiresToolCallReasoning(p) || !provider.AllowsEmptyReasoningFallback(p) {
		t.Fatal("thinking-enabled gateway must use DeepSeek tool reasoning replay")
	}
	req := p.(*client).buildRequest(provider.Request{Messages: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "read_file", Arguments: `{"path":"main.go"}`}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "read_file", Content: "package main"},
		{Role: provider.RoleAssistant, ReasoningContent: "read first", ToolCalls: []provider.ToolCall{{ID: "c2", Name: "read_file", Arguments: `{"path":"go.mod"}`}}},
		{Role: provider.RoleTool, ToolCallID: "c2", Name: "read_file", Content: "module demo"},
		{Role: provider.RoleAssistant, Content: "done", ReasoningContent: "do not replay"},
	}})
	if got := req.Messages[0].ReasoningContent; got == nil || *got != "" {
		t.Fatalf("empty fallback = %v", got)
	}
	if got := req.Messages[2].ReasoningContent; got == nil || *got != "read first" {
		t.Fatalf("captured replay = %v", got)
	}
	body, err := json.Marshal(req.Messages)
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Messages[4].ReasoningContent; got == nil || *got != "do not replay" {
		t.Fatalf("plain assistant reasoning = %v, want it replayed", got)
	}
	if !strings.Contains(string(body), "do not replay") {
		t.Fatalf("plain assistant reasoning missing from wire body: %s", body)
	}
}
