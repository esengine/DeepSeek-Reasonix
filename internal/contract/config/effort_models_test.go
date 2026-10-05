package config

import (
	"slices"
	"testing"
)

func TestDeclaredLaddersByEndpoint(t *testing.T) {
	cases := []struct {
		name, kind, baseURL, model string
		levels                     []string
		def                        string
	}{
		{"gpt-5.6 alias on OpenAI", "openai", "https://api.openai.com/v1", "gpt-5.6",
			[]string{"auto", "none", "low", "medium", "high", "xhigh", "max"}, "medium"},
		{"gpt-5.6 on the Responses wire", "responses", "https://api.openai.com/v1", "gpt-5.6-luna",
			[]string{"auto", "none", "low", "medium", "high", "xhigh", "max"}, "medium"},
		{"gpt-6-sol on Responses", "responses", "https://api.openai.com/v1", "gpt-6-sol",
			[]string{"auto", "none", "low", "medium", "high", "xhigh", "max"}, "medium"},
		{"gpt-6-astra has no none", "responses", "https://api.openai.com/v1", "gpt-6-astra",
			[]string{"auto", "low", "medium", "high", "xhigh", "max"}, "auto"},
		{"qwen3.8 on Model Studio", "openai", "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", "qwen3.8-flash",
			[]string{"auto", "none", "low", "medium", "xhigh"}, "xhigh"},
		{"qwen3.8 on a workspace host", "openai", "https://ws.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1", "qwen3.8-27b",
			[]string{"auto", "none", "low", "medium", "xhigh"}, "xhigh"},
		{"qwen3.8 on a relay keeps the standard rungs", "openai", "https://relay.example.com/v1", "qwen3.8-max",
			[]string{"auto", "none", "low", "medium"}, "auto"},
	}
	for _, tc := range cases {
		e := &ProviderEntry{Name: "p", Kind: tc.kind, BaseURL: tc.baseURL, Model: tc.model}
		got := EffortCapabilityForEntry(e)
		if !got.Supported || !slices.Equal(got.Levels, tc.levels) || got.Default != tc.def {
			t.Errorf("%s: got %+v, want levels %v default %s", tc.name, got, tc.levels, tc.def)
		}
	}
}

// On Chat Completions GPT-6 takes function calls only at effort none, and every
// turn carries tools: a ladder there would offer rungs that answer 400.
func TestGPT6DeclaresNoChatCompletionsLadder(t *testing.T) {
	e := &ProviderEntry{Name: "oai", Kind: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-6-sol"}
	if got := EffortCapabilityForEntry(e); got.Supported {
		t.Fatalf("gpt-6-sol on Chat Completions offers %v", got.Levels)
	}
	if got := RequestEffortLevels(e); got != nil {
		t.Fatalf("gpt-6-sol on Chat Completions projects %v", got)
	}
}

func TestQwen38CompatibilityValuesFoldOntoItsLadder(t *testing.T) {
	vendor := &ProviderEntry{Name: "q", Kind: "openai", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen3.8-max"}
	relay := &ProviderEntry{Name: "r", Kind: "openai", BaseURL: "https://relay.example.com/v1", Model: "qwen3.8-max"}
	for _, tc := range []struct {
		e           *ProviderEntry
		level, want string
	}{
		{vendor, "high", "xhigh"}, {vendor, "max", "xhigh"}, {vendor, "minimal", "low"}, {vendor, "none", "none"},
		{relay, "xhigh", "medium"}, {relay, "high", "medium"}, {relay, "max", "medium"},
	} {
		got, err := NormalizeEffort(tc.e, tc.level)
		if err != nil || got != tc.want {
			t.Errorf("%s %q = %q, %v; want %q", tc.e.BaseURL, tc.level, got, err, tc.want)
		}
	}
}

func TestRequestEffortLevelsProjectsTheResolvedLadder(t *testing.T) {
	declared := &ProviderEntry{Kind: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-5.6-sol",
		SupportedEfforts: []string{"low", "high"}}
	if got := RequestEffortLevels(declared); !slices.Equal(got, []string{"low", "high"}) {
		t.Fatalf("a declared list must win, got %v", got)
	}
	relay := &ProviderEntry{Kind: "openai", BaseURL: "https://relay.example.com/v1", Model: "gpt-5.6-sol"}
	if got := RequestEffortLevels(relay); !slices.Equal(got, []string{"none", "low", "medium", "high"}) {
		t.Fatalf("relay projects %v", got)
	}
	other := &ProviderEntry{Kind: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-5.6-sol",
		ReasoningProtocol: ReasoningProtocolDeepSeek}
	if got := RequestEffortLevels(other); got != nil {
		t.Fatalf("a different declared protocol must not inherit the ladder, got %v", got)
	}
}
