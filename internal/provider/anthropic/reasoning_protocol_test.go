package anthropic

import (
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestNewHonorsExplicitDeepSeekReasoningProtocol(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		protocol string
		want     bool
	}{
		{name: "custom opt in", baseURL: "https://gateway.example/v1", protocol: "deepseek", want: true},
		{name: "custom auto", baseURL: "https://gateway.example/v1", protocol: "auto", want: false},
		{name: "official auto", baseURL: "https://api.deepseek.com/anthropic", protocol: "auto", want: true},
		{name: "official opt out", baseURL: "https://api.deepseek.com/anthropic", protocol: "none", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := New(provider.Config{
				Name: "gateway", BaseURL: tc.baseURL, Model: "deepseek-v4-flash", APIKey: "k",
				Extra: map[string]any{"reasoning_protocol": tc.protocol, "thinking": "enabled", "vision": true},
			})
			if err != nil {
				t.Fatal(err)
			}
			c := p.(*client)
			if c.deepseek != tc.want {
				t.Fatalf("deepseek replay = %v, want %v", c.deepseek, tc.want)
			}
			if strings.Contains(tc.baseURL, "gateway.example") && !c.vision {
				t.Fatal("custom replay opt-in must not inherit official text-only constraint")
			}
		})
	}
}
