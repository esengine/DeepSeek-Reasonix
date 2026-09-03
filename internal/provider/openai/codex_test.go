package openai

import (
	"encoding/json"
	"testing"

	"reasonix/internal/provider"
)

func TestBuildRequestCodexCarriesSystemAsUser(t *testing.T) {
	sys := provider.Message{Role: provider.RoleSystem, Content: "agent system prompt"}
	tests := []struct {
		name    string
		baseURL string
		model   string
		want    string // first-message role on the wire
	}{
		{name: "official codex repro", baseURL: "https://api.openai.com/v1", model: "gpt-5.1-codex", want: "user"},
		{name: "codex-mini", baseURL: "https://api.openai.com/v1", model: "gpt-5.1-codex-mini", want: "user"},
		{name: "gateway codex", baseURL: "https://gateway.example/v1", model: "gpt-5.1-codex", want: "user"},
		{name: "non-codex openai", baseURL: "https://api.openai.com/v1", model: "gpt-5", want: "system"},
		{name: "non-codex chat", baseURL: "https://gateway.example/v1", model: "gpt-4o", want: "system"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := New(provider.Config{Name: "t", BaseURL: tc.baseURL, Model: tc.model, APIKey: "k"})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			body, err := json.Marshal(p.(*client).buildRequest(provider.Request{Messages: []provider.Message{sys, {Role: provider.RoleUser, Content: "hi"}}}))
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}
			var decoded struct {
				Messages []map[string]json.RawMessage `json:"messages"`
			}
			if err := json.Unmarshal(body, &decoded); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if len(decoded.Messages) != 2 {
				t.Fatalf("messages = %d, want 2", len(decoded.Messages))
			}
			if got := string(decoded.Messages[0]["role"]); got != `"`+tc.want+`"` {
				t.Fatalf("first message role = %s, want %q", got, tc.want)
			}
			if got := string(decoded.Messages[0]["content"]); got != `"agent system prompt"` {
				t.Fatalf("system content changed: %s", got)
			}
			if got := string(decoded.Messages[1]["role"]); got != `"user"` {
				t.Fatalf("second message role = %s, want user", got)
			}
		})
	}
}
