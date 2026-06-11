package openai

import "testing"

// TestIsDeepSeek pins the host-matching rule for DeepSeek: the canonical
// api.deepseek.com, any *.deepseek.com subdomain, but NOT the apex
// deepseek.com (a misconfiguration we explicitly reject).
func TestIsDeepSeek(t *testing.T) {
	for _, tc := range []struct {
		baseURL string
		want    bool
	}{
		// Canonical
		{"https://api.deepseek.com", true},
		{"https://api.deepseek.com/v1", true},
		{"https://api.deepseek.com/anthropic", true},
		// Regional subdomains under the apex
		{"https://eu.deepseek.com/v1", true},
		{"https://us.deepseek.com/v1", true},
		// Apex rejected (would require a user pointing their base_url at
		// the apex domain, which is a misconfiguration)
		{"https://deepseek.com/v1", false},
		{"https://deepseek.com", false},
		// Other vendors must not match
		{"https://api.minimaxi.com/v1", false},
		{"https://api.openai.com/v1", false},
		// Wrong-spelling TLDs (e.g. "deepseek.io") must not match
		{"https://api.deepseek.io", false},
		{"https://deepseek.io", false},
		// Garbage
		{"", false},
		{"not-a-url", false},
	} {
		if got := IsDeepSeek(tc.baseURL); got != tc.want {
			t.Errorf("IsDeepSeek(%q) = %v, want %v", tc.baseURL, got, tc.want)
		}
	}
}

// TestIsMiniMax pins the host-matching rule for MiniMax. The spelling is
// `minimaxi`, not `minimax` — the latter is reserved for any future
// minimax-branded gateway so the two never collide.
func TestIsMiniMax(t *testing.T) {
	for _, tc := range []struct {
		baseURL string
		want    bool
	}{
		// Canonical
		{"https://api.minimaxi.com", true},
		{"https://api.minimaxi.com/v1", true},
		{"https://api.minimaxi.com/anthropic", true},
		// Regional subdomains under the apex
		{"https://eu.minimaxi.com/v1", true},
		{"https://us.minimaxi.com/v1", true},
		// Apex rejected
		{"https://minimaxi.com/v1", false},
		{"https://minimaxi.com", false},
		// Other vendors must not match
		{"https://api.deepseek.com", false},
		{"https://api.openai.com/v1", false},
		// Wrong spelling — minimax, not minimaxi — must not match
		{"https://api.minimax.com/v1", false},
		{"https://api.minimax.example.com", false},
		// Garbage
		{"", false},
		{"not-a-url", false},
	} {
		if got := IsMiniMax(tc.baseURL); got != tc.want {
			t.Errorf("IsMiniMax(%q) = %v, want %v", tc.baseURL, got, tc.want)
		}
	}
}
