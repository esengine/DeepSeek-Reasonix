package config

import (
	"testing"
)

func TestCuratedProviderPresetDeepSeekReasoningProtocolScope(t *testing.T) {
	var cfg Config
	for _, preset := range CuratedProviderPresets() {
		for _, entry := range preset.Entries {
			if err := cfg.UpsertProvider(entry); err != nil {
				t.Fatalf("upsert preset %q: %v", preset.ID, err)
			}
		}
	}

	tests := []struct {
		ref  string
		want string
	}{
		{ref: "opencode-go/deepseek-v4-pro", want: ReasoningProtocolDeepSeek},
		{ref: "opencode-go/deepseek-v4-flash", want: ReasoningProtocolDeepSeek},
		{ref: "ollama-cloud/deepseek-v4-pro", want: ReasoningProtocolDeepSeek},
		{ref: "ollama-cloud/deepseek-v4-flash", want: ReasoningProtocolDeepSeek},
		{ref: "novita/deepseek/deepseek-v4-pro"},
		{ref: "novita/deepseek/deepseek-v4-flash"},
		{ref: "gmi/deepseek-ai/DeepSeek-V4-Pro"},
		{ref: "gmi/deepseek-ai/DeepSeek-V4-Flash"},
		{ref: "nvidia/deepseek-ai/deepseek-v4-pro"},
		{ref: "vercel-ai-gateway/deepseek/deepseek-v4-pro"},
	}
	for _, tc := range tests {
		t.Run(tc.ref, func(t *testing.T) {
			entry, ok := cfg.ResolveModel(tc.ref)
			if !ok {
				t.Fatalf("ResolveModel(%q) failed", tc.ref)
			}
			if got := ReasoningProtocolForEntry(entry); got != tc.want {
				t.Fatalf("ReasoningProtocolForEntry(%q) = %q, want %q", tc.ref, got, tc.want)
			}
		})
	}
}
