package config

import (
	"slices"
	"testing"
)

func glmRelay(model, protocol string) *ProviderEntry {
	return &ProviderEntry{Kind: "openai", BaseURL: "https://www.dmxapi.cn/v1", Model: model, ReasoningProtocol: protocol}
}

func TestGLMContractFollowsTheDeclaredProtocolNotTheHost(t *testing.T) {
	for _, tc := range []struct {
		name   string
		entry  *ProviderEntry
		levels []string
		def    string
		forces bool
	}{
		{"relay glm-5.3", glmRelay("glm-5.3", ReasoningProtocolGLM), []string{"auto", "low", "high", "max"}, "max", true},
		{"relay glm-5.3-flash", glmRelay("GLM-5.3-Flash", ReasoningProtocolGLM), []string{"auto", "low", "high", "max"}, "max", true},
		{"relay glm-5.2", glmRelay("glm-5.2", ReasoningProtocolGLM), []string{"auto", "none", "minimal", "low", "medium", "high", "xhigh", "max"}, "max", false},
		{"relay model outside the table", glmRelay("glm-4.5", ReasoningProtocolGLM), []string{"auto", "enabled", "disabled"}, "enabled", false},
		{"relay protocol openai", glmRelay("glm-5.3", ReasoningProtocolOpenAI), []string{"auto", "low", "medium", "high"}, "auto", false},
		{"relay protocol undeclared", glmRelay("glm-5.3", ""), nil, "", false},
		{"zhipu host", &ProviderEntry{Kind: "openai", BaseURL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-5.3"}, []string{"auto", "low", "high", "max"}, "max", true},
		{"per-model override", &ProviderEntry{Kind: "openai", BaseURL: "https://www.dmxapi.cn/v1", Model: "glm-5.3", ModelOverrides: map[string]ProviderModelOverride{"glm-5.3": {ReasoningProtocol: ReasoningProtocolGLM}}}, []string{"auto", "low", "high", "max"}, "max", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := tc.entry.forModel(tc.entry.Model)
			cap := EffortCapabilityForEntry(e)
			if !slices.Equal(cap.Levels, tc.levels) || cap.Default != tc.def {
				t.Fatalf("capability = %+v, want %v default %q", cap, tc.levels, tc.def)
			}
			if got := EffortForcesThinking(e); got != tc.forces {
				t.Fatalf("forces thinking = %v, want %v", got, tc.forces)
			}
		})
	}
}

func TestGLMContractMapsASavedDisabledOnARelay(t *testing.T) {
	for _, tc := range []struct{ model, want string }{
		{"glm-5.3", "low"},
		{"glm-5.2", "none"},
	} {
		e := glmRelay(tc.model, ReasoningProtocolGLM)
		e.Effort = "disabled"
		if got := EffectiveEffort(e); got != tc.want {
			t.Errorf("%s: stored disabled resolves to %q, want %q", tc.model, got, tc.want)
		}
		if got, err := NormalizeEffort(e, "disabled"); err != nil || got != tc.want {
			t.Errorf("%s: /effort disabled = %q/%v, want %q", tc.model, got, err, tc.want)
		}
	}
	plain := glmRelay("glm-4.5", ReasoningProtocolGLM)
	plain.Effort = "disabled"
	if got := EffectiveEffort(plain); got != "disabled" {
		t.Errorf("a model outside the table keeps the binary knob, got %q", got)
	}
}

func TestInheritedEffortNamesTheContractAsOfficial(t *testing.T) {
	relay := glmRelay("glm-5.3", ReasoningProtocolGLM)
	if !relay.InheritedEffortOfficial("glm-5.3") {
		t.Fatal("a relay declared glm should inherit the official contract")
	}
	if relay.InheritedEffortOfficial("glm-4.5") {
		t.Fatal("a model outside the table has no official levels")
	}
	typed := glmRelay("glm-5.3", ReasoningProtocolGLM)
	typed.SupportedEfforts = []string{"low", "high"}
	if typed.InheritedEffortOfficial("glm-5.3") {
		t.Fatal("typed connection levels take precedence over the contract")
	}
	if glmRelay("glm-5.3", "").InheritedEffortOfficial("glm-5.3") {
		t.Fatal("no declared protocol, no contract")
	}
}
