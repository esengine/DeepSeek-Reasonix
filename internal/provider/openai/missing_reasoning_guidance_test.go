package openai

import (
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestMissingToolCallReasoningGuidance(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		extra   map[string]any
		want    string // "" = generic guidance stays; non-empty = relay hint appended
	}{
		{
			name:    "official deepseek endpoint gets endpoint-behavior hint",
			baseURL: "https://api.deepseek.com/v1",
			want:    "api-docs.deepseek.com",
		},
		{
			name:    "official endpoint with thinking disabled stays silent",
			baseURL: "https://api.deepseek.com/v1",
			extra:   map[string]any{"thinking": "disabled"},
			want:    "",
		},
		{
			name:    "non-deepseek endpoint stays silent",
			baseURL: "https://relay.example.com/v1",
			want:    "",
		},
		{
			name:    "third-party relay with explicit deepseek protocol gets the hint",
			baseURL: "https://relay.example.com/v1",
			extra:   map[string]any{"reasoning_protocol": "deepseek"},
			want:    "not api.deepseek.com",
		},
		{
			name:    "third-party relay with plain chat protocol stays silent",
			baseURL: "https://relay.example.com/v1",
			extra:   map[string]any{"reasoning_protocol": "none"},
			want:    "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := New(provider.Config{Name: "p", BaseURL: tc.baseURL, Model: "deepseek-v4-flash", APIKey: "k", Extra: tc.extra})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			got := p.(*client).MissingToolCallReasoningGuidance()
			if tc.want == "" {
				if got != "" {
					t.Fatalf("guidance = %q, want empty", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("guidance = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}
