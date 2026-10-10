package config

import "testing"

func TestEffortFieldFollowsTheResolvedProtocol(t *testing.T) {
	for _, tc := range []struct {
		name string
		e    ProviderEntry
		want string
	}{
		{"chat, nothing declared", ProviderEntry{Kind: "openai", BaseURL: "https://relay.example.com/v1", Model: "gpt-5.5"}, "reasoning_effort"},
		{"chat, openai declared", ProviderEntry{Kind: "openai", BaseURL: "https://relay.example.com/v1", Model: "m", ReasoningProtocol: "openai"}, "reasoning_effort"},
		{"chat, glm declared", ProviderEntry{Kind: "openai", BaseURL: "https://relay.example.com/v1", Model: "m", ReasoningProtocol: "glm"}, ""},
		{"chat, none declared", ProviderEntry{Kind: "openai", BaseURL: "https://relay.example.com/v1", Model: "m", ReasoningProtocol: "none"}, ""},
		{"chat on Zhipu, auto", ProviderEntry{Kind: "openai", BaseURL: "https://api.z.ai/api/paas/v4", Model: "glm-5.3"}, ""},
		{"chat on MiniMax, auto", ProviderEntry{Kind: "openai", BaseURL: "https://api.minimaxi.com/v1", Model: "MiniMax-M3"}, ""},
		{"chat on LongCat, auto", ProviderEntry{Kind: "openai", BaseURL: "https://api.longcat.chat/openai", Model: "LongCat-Flash"}, ""},
		{"chat on DeepSeek, auto", ProviderEntry{Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-pro"}, ""},
		{"responses, nothing declared", ProviderEntry{Kind: "responses", BaseURL: "https://www.dmxapi.cn/v1", Model: "gpt-5.5"}, "reasoning.effort"},
		{"responses, glm declared", ProviderEntry{Kind: "responses", BaseURL: "https://www.dmxapi.cn/v1", Model: "m", ReasoningProtocol: "glm"}, "reasoning.effort"},
		{"responses, none declared", ProviderEntry{Kind: "responses", BaseURL: "https://www.dmxapi.cn/v1", Model: "m", ReasoningProtocol: "none"}, ""},
		{"anthropic first party", ProviderEntry{Kind: "anthropic", BaseURL: "https://api.anthropic.com", Model: "claude-opus-4-8"}, "output_config.effort"},
		{"anthropic gateway, auto", ProviderEntry{Kind: "anthropic", BaseURL: "https://gw.example.com", Model: "m"}, ""},
		{"anthropic, none declared", ProviderEntry{Kind: "anthropic", BaseURL: "https://api.anthropic.com", Model: "m", ReasoningProtocol: "none"}, ""},
		{"decision wire", ProviderEntry{Kind: "typesafe"}, ""},
	} {
		if got := EffortFieldForEntry(&tc.e); got != tc.want {
			t.Errorf("%s: EffortFieldForEntry = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestNoneProtocolSendsNoEffortOnAnthropic(t *testing.T) {
	e := &ProviderEntry{
		Name: "p", Kind: "anthropic", BaseURL: "https://api.anthropic.com", Model: "claude-opus-4-8",
		ReasoningProtocol: "none", Effort: "high", SupportedEfforts: []string{"low", "high"},
	}
	if got := EffectiveEffort(e); got != "" {
		t.Fatalf("EffectiveEffort = %q under reasoning_protocol=none, want none sent", got)
	}
}
