package openai

import (
	"testing"

	"reasonix/internal/provider"
)

// A third-party endpoint can serve a DeepSeek model under its own effort scale.
// Declaring supported_efforts is how the config says so, and our built-in
// DeepSeek vocabulary must not override that declaration (#7273) — the same
// rule the "low" level already followed.
func TestDeepSeekThinkingHonorsDeclaredEffortVocabulary(t *testing.T) {
	p, err := New(provider.Config{
		Name:    "proxy",
		BaseURL: "https://proxy.example.com/v1",
		Model:   "deepseek-v4-flash",
		APIKey:  "test",
		Extra: map[string]any{
			"effort":             "medium",
			"reasoning_protocol": "deepseek",
			"supported_efforts":  []string{"low", "medium", "high", "none"},
		},
	})
	if err != nil {
		t.Fatalf("New with declared effort vocabulary: %v", err)
	}
	if got := p.(*client).buildRequest(provider.Request{}).ReasoningEffort; got != "medium" {
		t.Fatalf("reasoning_effort = %q, want the declared level forwarded", got)
	}
}

// Without a declaration there is nothing to defer to, so an unknown level is
// still rejected rather than silently forwarded to the official endpoint.
func TestDeepSeekThinkingRejectsUndeclaredEffort(t *testing.T) {
	_, err := New(provider.Config{
		Name:    "official",
		BaseURL: "https://api.deepseek.com",
		Model:   "deepseek-v4-flash",
		APIKey:  "test",
		Extra: map[string]any{
			"effort":             "medium",
			"reasoning_protocol": "deepseek",
		},
	})
	if err == nil {
		t.Fatal("New with an undeclared effort level = nil error, want rejection")
	}
}

// A declaration that does not list the requested level is still a rejection:
// honoring the config means honoring what it actually says.
func TestDeepSeekThinkingRejectsEffortOutsideDeclaration(t *testing.T) {
	_, err := New(provider.Config{
		Name:    "proxy",
		BaseURL: "https://proxy.example.com/v1",
		Model:   "deepseek-v4-flash",
		APIKey:  "test",
		Extra: map[string]any{
			"effort":             "medium",
			"reasoning_protocol": "deepseek",
			"supported_efforts":  []string{"low", "high"},
		},
	})
	if err == nil {
		t.Fatal("New with an effort outside the declaration = nil error, want rejection")
	}
}
