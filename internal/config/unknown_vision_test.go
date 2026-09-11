package config

import "testing"

func TestUnknownDeepSeekVisionOverride(t *testing.T) {
	model := "deepseek-v5-flash"
	enabled := true
	e := ProviderEntry{Kind: "openai", BaseURL: "https://api.deepseek.com", Model: model}
	r := NewModelCapabilityResolver()
	if got := r.Resolve(&e); !got.ImageInputEnableAllowed || got.State != CapabilityUnknown {
		t.Fatalf("unknown model should permit manual declaration: %+v", got)
	}
	e.ModelOverrides = map[string]ProviderModelOverride{model: {Vision: &enabled}}
	if got := r.Resolve(&e); !got.ImageInputEnableAllowed || got.State != CapabilitySupported {
		t.Fatalf("override rejected: %+v", got)
	}
	e.Name = "deepseek-test"
	e.Model = ""
	e.Models = []string{"DeepSeek-V5-Flash", model}
	cfg := Config{Providers: []ProviderEntry{e}}
	resolved, ok := cfg.ResolveModel(e.Name + "/" + model)
	if !ok || resolved.Model != model || !EffectiveVision(resolved) {
		t.Fatalf("lowercase model and its vision override did not resolve: %+v", resolved)
	}
	upper, ok := cfg.ResolveModel(e.Name + "/" + e.Models[0])
	if !ok || EffectiveVision(upper) {
		t.Fatal("uppercase model inherited lowercase vision override")
	}
}

func TestCaseDistinctModelOverridesAreIsolated(t *testing.T) {
	on := true
	for _, configured := range []string{"model-a", "Model-A"} {
		e := ProviderEntry{Models: []string{"model-a", "Model-A"}, VisionModels: []string{configured}, ModelOverrides: map[string]ProviderModelOverride{configured: {Vision: &on, ContextWindow: 1234, MaxOutputTokens: 123, ReasoningProtocol: "deepseek"}}}
		for _, model := range e.Models {
			_, matched := e.modelOverrideForModel(model)
			if matched != (model == configured) || e.HasVisionModel(model) != (model == configured) {
				t.Fatalf("%s inherited settings from %s", model, configured)
			}
		}
	}
}
