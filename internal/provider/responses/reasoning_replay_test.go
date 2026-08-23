package responses

import (
	"testing"

	"reasonix/internal/provider"
)

func TestEmptyReasoningFallbackIsScopedToThinkingStatelessVendors(t *testing.T) {
	deepseek := New(Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash"})
	if !provider.AllowsEmptyReasoningFallback(deepseek) {
		t.Fatal("DeepSeek Responses provider must accept tool turns whose output omitted reasoning")
	}
	mimo := New(Config{Name: "mimo", BaseURL: "https://api.xiaomimimo.com/v1", Model: "mimo-v2.5-pro"})
	if !provider.AllowsEmptyReasoningFallback(mimo) {
		t.Fatal("MiMo Responses provider must accept its optional tool-call reasoning")
	}
	disabled := New(Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-pro", Effort: "none"})
	if provider.RequiresToolCallReasoning(disabled) || provider.WarnOnMissingToolCallReasoning(disabled) {
		t.Fatal("reasoning-disabled Responses provider must not require or diagnose missing reasoning")
	}
	unknown := New(Config{Name: "other", BaseURL: "https://example.com", Model: "m"})
	if provider.AllowsEmptyReasoningFallback(unknown) {
		t.Fatal("unknown Responses endpoint must not inherit a vendor-specific empty fallback")
	}
}

func TestStatelessRequestReplaysToolPairWithoutFabricatingReasoning(t *testing.T) {
	client := New(Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash"}).(*client)
	body, _, _ := client.buildRequestBody(provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "run"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "echo", Arguments: `{"text":"hi"}`}}},
		{Role: provider.RoleTool, ToolCallID: "call_1", Name: "echo", Content: "hi"},
	}})

	items := body["input"].([]map[string]any)
	if len(items) != 3 || items[1]["type"] != "function_call" || items[2]["type"] != "function_call_output" {
		t.Fatalf("input = %#v, want user/call/output", items)
	}
	for _, item := range items {
		if item["type"] == "reasoning" {
			t.Fatalf("missing provider reasoning was fabricated: %#v", item)
		}
	}
}
