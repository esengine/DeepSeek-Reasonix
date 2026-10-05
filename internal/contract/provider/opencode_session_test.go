package provider

import (
	"net/http"
	"testing"
)

func TestApplyOpenCodeGoIdentityScope(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://opencode.ai/zen/go/v1/chat/completions", true},
		{"https://OpenCode.ai/zen/go/v1/messages", true},
		{"https://opencode.ai/zen/v1/chat/completions", false},
		{"https://opencode.ai/zen/gopher/v1/chat/completions", false},
		{"https://relay.example.com/zen/go/v1/chat/completions", false},
	}
	for _, tc := range cases {
		req, _ := http.NewRequest(http.MethodPost, tc.url, nil)
		ApplyOpenCodeGoIdentity(req, "s1")
		if got := req.Header.Get("x-opencode-session") == "s1"; got != tc.want {
			t.Errorf("%s: session applied = %v, want %v", tc.url, got, tc.want)
		}
	}
}

func TestApplyOpenCodeGoIdentityKeepsConfiguredHeaders(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://opencode.ai/zen/go/v1/chat/completions", nil)
	req.Header.Set("User-Agent", "custom/1.0")
	req.Header.Set("x-opencode-session", "mine")
	ApplyOpenCodeGoIdentity(req, "s1")
	if req.Header.Get("User-Agent") != "custom/1.0" || req.Header.Get("x-opencode-session") != "mine" {
		t.Fatalf("headers = %v, want the configured values kept", req.Header)
	}
}
