package boot

import (
	"errors"
	"reflect"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/provider"
)

func TestOpenCodeGoDeepSeekReasoningContract(t *testing.T) {
	for _, model := range []string{"deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp"} {
		for _, kind := range []string{"openai", "anthropic", "responses"} {
			t.Run(kind+"/"+model, func(t *testing.T) {
				endpoint := map[string]string{"openai": "chat/completions", "anthropic": "messages", "responses": "responses"}[kind]
				e := config.ProviderEntry{Name: "opencode-go", Kind: kind, BaseURL: "https://opencode.ai/zen/go/v1", RequestURL: "https://opencode.ai/zen/go/v1/" + endpoint, Model: model, Thinking: "enabled", Effort: "max"}
				before := config.ReasoningCapabilityForEntry(&e)
				p, err := NewProvider(&e)
				if err != nil {
					t.Fatal(err)
				}
				after := p.(provider.ReasoningProvider).ReasoningCapability()
				if !reflect.DeepEqual(before.IDs(), after.IDs()) || after.Validate(model, "max") != nil {
					t.Fatalf("catalog %v differs from adapter %v", before, after)
				}
				if !provider.RequiresToolCallReasoning(p) {
					t.Fatal("DeepSeek tool reasoning replay is not enabled")
				}
			})
		}
	}
}

func TestOpenCodeGoContractDoesNotOverrideCustomEndpoint(t *testing.T) {
	e := config.ProviderEntry{Name: "opencode-go", Kind: "anthropic", BaseURL: "https://opencode.ai/zen/go", RequestURL: "https://custom.example/v1/messages", Model: "deepseek-v4-pro", Thinking: "enabled", Effort: "max"}
	_, err := NewProvider(&e)
	var unsupported *provider.UnsupportedReasoningEffort
	if !errors.As(err, &unsupported) {
		t.Fatalf("custom endpoint must retain its own binary contract: %v", err)
	}
}
