package openai

import (
	"testing"

	"reasonix/internal/provider"
)

func TestExplicitKimiK3GatewayRequestContract(t *testing.T) {
	p, err := New(provider.Config{
		Name: "custom-kimi-gateway", BaseURL: "https://gateway.example.com/v1", Model: "kimi-k3",
		Extra: map[string]any{"effort": "high", "reasoning_protocol": "kimi-k3"},
	})
	if err != nil {
		t.Fatalf("New explicit Kimi K3 gateway: %v", err)
	}
	if !provider.RequiresReasoningRoundTrip(p) {
		t.Fatal("explicit Kimi K3 protocol must retain reasoning for complete assistant-message replay")
	}
	req := p.(*client).buildRequest(provider.Request{
		Temperature: provider.TemperaturePtr(0.2), MaxTokens: 4096,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "question"},
			{Role: provider.RoleAssistant, Content: "answer", ReasoningContent: "gateway reasoning"},
		},
	})
	if req.ReasoningEffort != "high" || req.Temperature != nil || req.MaxTokens != 0 || req.MaxCompletionTokens != 4096 {
		t.Fatalf("explicit Kimi K3 request shape = %+v", req)
	}
	if got := req.Messages[1].ReasoningContent; got == nil || *got != "gateway reasoning" {
		t.Fatalf("explicit Kimi K3 reasoning_content = %v, want gateway reasoning", got)
	}
	if _, err := New(provider.Config{
		Name: "custom-kimi-gateway", BaseURL: "https://gateway.example.com/v1", Model: "kimi-k3",
		Extra: map[string]any{"effort": "medium", "reasoning_protocol": "kimi-k3"},
	}); err == nil {
		t.Fatal("explicit Kimi K3 protocol should reject unsupported medium effort")
	}
	auto, err := New(provider.Config{
		Name: "custom-kimi-gateway", BaseURL: "https://gateway.example.com/v1", Model: "kimi-k3",
		Extra: map[string]any{
			"reasoning_protocol": "kimi-k3",
			"supported_efforts":  []string{"medium", "ultra"},
		},
	})
	if err != nil {
		t.Fatalf("New Kimi K3 with dormant custom efforts: %v", err)
	}
	if got := auto.(*client).buildRequest(provider.Request{}).ReasoningEffort; got != "max" {
		t.Fatalf("Kimi K3 protocol default effort = %q, want max", got)
	}
}
