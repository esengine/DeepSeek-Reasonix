//go:build live

package responses

import (
	"os"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// TestRealDeepSeekResponsesWebSearch exercises the official stateless
// Responses endpoint with its provider-executed web_search tool. It is
// credential-gated and build-tagged so ordinary CI remains deterministic.
func TestRealDeepSeekResponsesWebSearch(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set — skipping live probe")
	}

	p := New(Config{
		Name:            "deepseek-responses",
		BaseURL:         "https://api.deepseek.com",
		Model:           "deepseek-v4-flash",
		APIKey:          key,
		Effort:          "disabled",
		WebSearch:       true,
		MaxOutputTokens: 256,
		KeyEnv:          "DEEPSEEK_API_KEY",
	})
	chunks := collect(t, p, provider.Request{Messages: []provider.Message{{
		Role:    provider.RoleUser,
		Content: "Search the web for the latest DeepSeek API documentation update and reply with one source URL.",
	}}, MaxTokens: 256})
	var text strings.Builder
	for _, chunk := range chunks {
		if chunk.Type == provider.ChunkText {
			text.WriteString(chunk.Text)
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		t.Fatalf("web_search returned no assistant text")
	}
	t.Logf("web_search: text=%d chunks=%d", len(text.String()), len(chunks))
}
