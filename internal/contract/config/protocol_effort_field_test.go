package config

import (
	"slices"
	"testing"
)

func TestProtocolsDeclareWhereEffortLands(t *testing.T) {
	want := map[string]string{
		"openai":    "reasoning_effort",
		"responses": "reasoning.effort",
		"anthropic": "output_config.effort",
		"typesafe":  "",
	}
	seen := 0
	for _, p := range Protocols() {
		field, ok := want[p.Kind]
		if !ok {
			t.Errorf("protocol %q has no expectation here: declare where its effort lands", p.Kind)
			continue
		}
		seen++
		if p.EffortField != field {
			t.Errorf("%s: EffortField = %q, want %q", p.Kind, p.EffortField, field)
		}
	}
	if seen != len(want) {
		t.Errorf("saw %d protocols, want %d", seen, len(want))
	}
}

func TestEffortFieldFollowsAliases(t *testing.T) {
	p, ok := ProtocolFor("dashscope-responses")
	if !ok || p.EffortField != "reasoning.effort" {
		t.Fatalf("dashscope-responses resolved to %+v", p)
	}
}

func TestRequestEffortLevelsMatchTheMenuForADeclaredProtocol(t *testing.T) {
	for _, kind := range []string{"openai", "responses"} {
		e := &ProviderEntry{Name: "p", Kind: kind, BaseURL: "https://relay.example.com/v1", Model: "gpt-5.5", ReasoningProtocol: "openai"}
		menu := EffortCapabilityForEntry(e)
		if !menu.Supported {
			t.Fatalf("%s: a declared openai protocol must offer levels", kind)
		}
		got := RequestEffortLevels(e)
		if want := menu.Levels[1:]; !slices.Equal(got, want) {
			t.Errorf("%s: RequestEffortLevels = %v, want the menu without auto %v", kind, got, want)
		}
	}
}

func TestNoneProtocolSendsNoEffortOnAnyKind(t *testing.T) {
	for _, kind := range []string{"openai", "responses"} {
		e := &ProviderEntry{
			Name: "p", Kind: kind, BaseURL: "https://relay.example.com/v1", Model: "gpt-5.5",
			ReasoningProtocol: "none", SupportedEfforts: []string{"low", "high"}, Effort: "high",
		}
		if got := EffectiveEffort(e); got != "" {
			t.Errorf("%s: EffectiveEffort = %q under reasoning_protocol=none, want none sent", kind, got)
		}
	}
}
