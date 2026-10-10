package config

import "testing"

func TestTypedOnOffMenuOnDeclaredGLMIsNotTranslatedOntoTheContract(t *testing.T) {
	for _, model := range []string{"glm-5.2", "glm-5.3", "glm-5.3-flash"} {
		e := &ProviderEntry{
			Kind: "openai", BaseURL: "https://www.dmxapi.cn/v1", Model: model,
			ReasoningProtocol: ReasoningProtocolGLM,
			SupportedEfforts:  []string{"enabled", "disabled"}, DefaultEffort: "enabled",
			Effort: "disabled",
		}
		if got := EffortDisplay(e); got != "disabled" {
			t.Errorf("%s: EffortDisplay = %q, want disabled", model, got)
		}
		if got := EffectiveEffort(e); got != "disabled" {
			t.Errorf("%s: EffectiveEffort = %q, want disabled", model, got)
		}
		if EffortForcesThinking(e) {
			t.Errorf("%s: a typed menu offering disabled must not read as forced thinking", model)
		}
	}
}
