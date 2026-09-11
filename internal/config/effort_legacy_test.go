package config

import (
	"reasonix/internal/provider"
	"testing"
)

func TestStoredDeepSeekEffortRemainsConstructible(t *testing.T) {
	for _, kind := range []string{"openai", "anthropic"} {
		for _, effort := range []string{"medium", "xhigh"} {
			e := ProviderEntry{Name: "saved", Kind: kind, BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", Effort: effort, ReasoningProtocol: "deepseek"}
			got := EffectiveEffort(&e)
			if got != "high" {
				t.Fatalf("%s %s -> %s", kind, effort, got)
			}
			if _, err := provider.New(kind, provider.Config{Name: e.Name, BaseURL: e.BaseURL, Model: e.Model, Extra: map[string]any{"effort": got, "reasoning_protocol": "deepseek"}}); err != nil {
				t.Fatal(err)
			}
			if _, err := NormalizeEffort(&e, effort); err == nil {
				t.Fatal("new undeclared selection accepted")
			}
			if e.Effort != effort {
				t.Fatal("stored config mutated")
			}
			e.SupportedEfforts = []string{"medium", "high"}
			if EffectiveEffort(&e) != effort {
				t.Fatal("explicit deployment declaration changed")
			}
		}
	}
}
