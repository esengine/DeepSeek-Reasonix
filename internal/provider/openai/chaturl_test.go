package openai

import "testing"

// The v9 migration clears a standard request_url override instead of pinning the
// canonical URL, so the derived endpoint must stay byte-identical to it.
func TestOfficialDeepSeekDerivedChatURLMatchesMigrationTarget(t *testing.T) {
	if got := resolveOpenAIChatURL("https://api.deepseek.com", nil); got != "https://api.deepseek.com/chat/completions" {
		t.Fatalf("derived chat URL = %q", got)
	}
	if got := normalizeChatURL("https://api.deepseek.com", ""); got != "https://api.deepseek.com/chat/completions" {
		t.Fatalf("normalized chat URL = %q", got)
	}
}

// Some provider editors mirror base_url into every endpoint field, leaving both
// overrides pinned to the API root. An OpenAI-compatible route cannot be served
// from that root: using it verbatim POSTs to "<base>/v1" and a local gateway
// answers "404 page not found". A mirrored root must therefore resolve to the
// canonical chat endpoint instead of being treated as an exact endpoint.
func TestEndpointOverrideMirroringBaseURLResolvesCanonicalChatURL(t *testing.T) {
	const base = "http://127.0.0.1:8000/v1"
	const want = "http://127.0.0.1:8000/v1/chat/completions"
	cases := []struct {
		name  string
		extra map[string]any
	}{
		{"request_url mirrors base_url", map[string]any{"request_url": base}},
		{"request_url mirrors base_url with trailing slash", map[string]any{"request_url": base + "/"}},
		{"chat_url mirrors base_url", map[string]any{"chat_url": base}},
		{"both overrides mirror base_url", map[string]any{"request_url": base, "chat_url": base}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveOpenAIChatURL(base, tc.extra); got != want {
				t.Fatalf("chat URL = %q, want %q", got, want)
			}
		})
	}
}

// Only an override that merely repeats the base URL is corrected; genuine exact
// endpoints stay user-owned.
func TestEndpointOverrideDistinctFromBaseURLStaysVerbatim(t *testing.T) {
	const base = "http://127.0.0.1:8000/v1"
	cases := []struct {
		name       string
		requestURL string
	}{
		{"sibling custom route", "http://127.0.0.1:8000/custom/chat/completions"},
		{"query override", "http://127.0.0.1:8000/v1?token=1"},
		{"fragment override", "http://127.0.0.1:8000/v1#debug"},
		{"other host", "https://gateway.test/v1"},
		{"other scheme", "https://127.0.0.1:8000/v1"},
		{"deeper path", "http://127.0.0.1:8000/v1/openai"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveOpenAIChatURL(base, map[string]any{"request_url": tc.requestURL})
			if got != tc.requestURL {
				t.Fatalf("chat URL = %q, want verbatim %q", got, tc.requestURL)
			}
		})
	}
}
