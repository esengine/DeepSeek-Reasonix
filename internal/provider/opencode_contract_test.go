package provider

import (
	"reflect"
	"testing"
)

func TestOpenCodeGoContractUsesExactEffectiveEndpointAndModel(t *testing.T) {
	for _, kind := range []string{"openai", "anthropic", "responses"} {
		endpoint := map[string]string{"openai": "chat/completions", "anthropic": "messages", "responses": "responses"}[kind]
		url := "https://opencode.ai/zen/go/v1/" + endpoint
		for _, model := range []string{"deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp"} {
			c, ok := LookupOpenCodeGoContract(kind, "https://custom.example", url, "", model)
			if !ok || c.RecommendedRoute != OpenCodeGoRouteChat || c.Reasoning.Default != "high" || c.Reasoning.Validate(model, "max") != nil {
				t.Fatalf("%s %s: %+v %v", kind, model, c, ok)
			}
			for _, custom := range []string{url + "?route=user", url + "#user", url + "/custom", "https://custom.example/v1/" + endpoint} {
				if _, ok := LookupOpenCodeGoContract(kind, "https://opencode.ai/zen/go/v1", custom, "", model); ok {
					t.Fatalf("custom endpoint inherited contract: %s", custom)
				}
			}
		}
		if _, ok := LookupOpenCodeGoContract(kind, "", url, "", "deepseek-v4-pro-custom"); ok {
			t.Fatal("unknown model was guessed from its prefix")
		}
		if _, ok := LookupOpenCodeGoContract(kind, "", url, "", "DeepSeek-v4-pro"); ok {
			t.Fatal("model IDs must be exact")
		}
	}
}

func TestOpenCodeGoDiscoveryFiltersTheActualRequestRoute(t *testing.T) {
	models := []string{"deepseek-v4-pro", "qwen3.8-max", "grok-4.5", "user-custom"}
	got := FilterOpenCodeGoRequestModels("openai", "https://opencode.ai/zen/go/v1", "https://opencode.ai/zen/go/v1/chat/completions", "", models)
	if !reflect.DeepEqual(got, []string{"deepseek-v4-pro", "qwen3.8-max"}) {
		t.Fatal(got)
	}
	got = FilterOpenCodeGoRequestModels("openai", "https://opencode.ai/zen/go/v1", "https://custom.example/v1/chat/completions", "", models)
	if !reflect.DeepEqual(got, models) {
		t.Fatal("custom discovery was filtered using an unrelated official base")
	}
}

func TestApplyOpenCodeGoContractDoesNotMutateCallerDeclarations(t *testing.T) {
	extra := map[string]any{"thinking": "enabled"}
	cfg := Config{BaseURL: "https://opencode.ai/zen/go", Model: "deepseek-v4-pro", Extra: extra}
	resolved := ApplyOpenCodeGoContract("anthropic", cfg)
	if len(extra) != 1 || resolved.Extra["reasoning_protocol"] != "deepseek" || resolved.Extra["default_effort"] != "high" {
		t.Fatalf("contract injection mutated caller or lost default: %+v", resolved.Extra)
	}
	extra["reasoning_protocol"] = "custom"
	extra["supported_efforts"] = []string{"custom-max"}
	resolved = ApplyOpenCodeGoContract("anthropic", cfg)
	if resolved.Extra["reasoning_protocol"] != "custom" || !reflect.DeepEqual(resolved.Extra["supported_efforts"], []string{"custom-max"}) {
		t.Fatal("explicit declaration was overwritten")
	}
}
