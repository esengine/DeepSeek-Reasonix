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

func TestExplicitGatewayReplayContractWithoutModelGuess(t *testing.T) {
	message := provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c", Name: "echo", Arguments: `{}`}}}
	history := []provider.Message{{Role: provider.RoleUser, Content: "go"}, message, {Role: provider.RoleTool, Name: "echo", ToolCallID: "c", Content: "done"}}
	for _, explicit := range []bool{false, true} {
		cfg := Config{BaseURL: "https://gateway.example/v1", Model: "deepseek-v4-flash", Mode: "stateless", Effort: "high"}
		if explicit {
			cfg.Extra = map[string]any{"reasoning_protocol": "deepseek"}
		}
		p := New(cfg)
		if provider.RequiresToolCallReasoning(p) != explicit {
			t.Fatal("explicit contract not honored or model name inferred strictness")
		}
		if explicit {
			if !provider.CanReplayAssistantMessage(p, message) {
				t.Fatal("healthy missing-item fallback changed")
			}
			projected, changed := provider.ProjectReasoningStrippedMessages(p, history)
			if !changed || len(projected) != 1 {
				t.Fatalf("rejected missing proof cannot recover: %+v", projected)
			}
		}
	}
}
