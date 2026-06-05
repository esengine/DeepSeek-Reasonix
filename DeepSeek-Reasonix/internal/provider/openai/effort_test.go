package openai

import (
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func newClient(t *testing.T, baseURL, effort string) *client {
	t.Helper()
	extra := map[string]any{}
	if effort != "" {
		extra["effort"] = effort
	}
	p, err := New(provider.Config{Name: "p", BaseURL: baseURL, Model: "m", APIKey: "k", Extra: extra})
	if err != nil {
		t.Fatalf("New(%q, effort=%q): %v", baseURL, effort, err)
	}
	return p.(*client)
}

func TestEffortNormalization(t *testing.T) {
	const mimo = "https://api.xiaomimimo.com/v1"
	const deepseek = "https://api.deepseek.com/v1"

	tests := []struct {
		base, effort, want string
	}{
		{mimo, "max", "high"}, // DeepSeek-ism clamped to the OpenAI ceiling — MiMo 400s on "max"
		{mimo, "high", "high"},
		{mimo, "medium", "medium"},
		{mimo, "low", "low"},
		{mimo, "MAX", "high"}, // case-insensitive
		{mimo, "", ""},        // unset stays omitted
		{deepseek, "max", "max"},
		{deepseek, "high", "high"},
		{deepseek, "", "high"}, // DeepSeek default depth
	}
	for _, tc := range tests {
		if got := newClient(t, tc.base, tc.effort).effort; got != tc.want {
			t.Errorf("base=%s effort=%q: got %q, want %q", tc.base, tc.effort, got, tc.want)
		}
	}
}

func TestEffortInvalidRejected(t *testing.T) {
	_, err := New(provider.Config{
		Name: "p", BaseURL: "https://api.xiaomimimo.com/v1", Model: "m", APIKey: "k",
		Extra: map[string]any{"effort": "turbo"},
	})
	if err == nil || !strings.Contains(err.Error(), "low, medium, or high") {
		t.Fatalf("expected a low/medium/high validation error, got: %v", err)
	}
}
