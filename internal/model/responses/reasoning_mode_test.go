package responses

import (
	"testing"

	"reasonix/internal/contract/provider"
)

func reasoningOf(t *testing.T, cfg Config, mode string) map[string]any {
	t.Helper()
	c := New(cfg).(*client)
	body, _, _ := c.buildRequestBody(provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		Mode:     mode,
	})
	reasoning, _ := body["reasoning"].(map[string]any)
	return reasoning
}

func TestDeclaredModeRidesReasoningBesideEffort(t *testing.T) {
	cfg := Config{BaseURL: "https://api.openai.com/v1", Model: "gpt-5.6-sol", Effort: "high",
		ReasoningModes: map[string]string{"pro": "pro"}}
	got := reasoningOf(t, cfg, "pro")
	if got["mode"] != "pro" || got["effort"] != "high" {
		t.Fatalf("reasoning = %v, want mode pro beside effort high", got)
	}
	cfg.Effort = ""
	if got := reasoningOf(t, cfg, "pro"); got["mode"] != "pro" || got["effort"] != nil {
		t.Fatalf("reasoning without effort = %v, want mode alone", got)
	}
}

func TestUndeclaredModeNeverReachesTheWire(t *testing.T) {
	declared := Config{BaseURL: "https://api.openai.com/v1", Model: "gpt-5.6-sol", Effort: "high",
		ReasoningModes: map[string]string{"pro": "pro"}}
	undeclared := Config{BaseURL: "https://relay.example.com/v1", Model: "gpt-5.6-sol", Effort: "high"}
	for _, tc := range []struct {
		name string
		cfg  Config
		mode string
	}{
		{"off", declared, ""},
		{"a mode the endpoint did not declare", declared, "deep"},
		{"an endpoint that declared none", undeclared, "pro"},
	} {
		if got := reasoningOf(t, tc.cfg, tc.mode); got["mode"] != nil {
			t.Errorf("%s: reasoning = %v, want no mode", tc.name, got)
		}
	}
}
