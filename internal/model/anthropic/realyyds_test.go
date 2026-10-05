//go:build live

package anthropic

import (
	"os"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

// TestRealYYDSAnthropicPromptCache pins the reason yyds-anthropic exists.
// Claude caches only where cache_control marks a breakpoint, and this package
// alone serializes it — the same model on an openai-kind entry bills every
// prompt token at full input rate. A cache read on the second call is the only
// evidence the breakpoint reached the gateway.
func TestRealYYDSAnthropicPromptCache(t *testing.T) {
	key := os.Getenv("YYDS_API_KEY")
	if key == "" {
		t.Skip("YYDS_API_KEY not set — skipping live probe")
	}

	p, err := New(provider.Config{
		Name:    "yyds-anthropic",
		BaseURL: "https://yyds.yy2hd.com/v1",
		Model:   "claude-opus-5",
		APIKey:  key,
		Extra:   map[string]any{"api_key_env": "YYDS_API_KEY", "effort": "max"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Over Opus's 1024-token minimum cacheable prefix, and stable across both
	// calls the way a session's system prompt is.
	system := strings.Repeat("You are a meticulous Go reviewer for Reasonix. "+
		"State invariants exactly and never speculate. ", 90)
	base := []provider.Message{
		{Role: provider.RoleSystem, Content: system},
		{Role: provider.RoleUser, Content: "Reply with the single word: one"},
	}

	first := collectLiveDeepSeekTurn(t, p, provider.Request{Messages: base, MaxTokens: 32})
	t.Logf("call#1 prompt=%d cache_hit=%d", first.promptTokens, first.cacheHitTokens)

	// Turn two appends to turn one, so the cached prefix is everything above.
	grown := append(append([]provider.Message{}, base...),
		provider.Message{Role: provider.RoleAssistant, Content: first.text},
		provider.Message{Role: provider.RoleUser, Content: "Reply with the single word: two"})

	second := collectLiveDeepSeekTurn(t, p, provider.Request{Messages: grown, MaxTokens: 32})
	t.Logf("call#2 prompt=%d cache_hit=%d", second.promptTokens, second.cacheHitTokens)

	if second.cacheHitTokens == 0 {
		t.Fatalf("no cache read on the appended turn: prompt=%d hit=0 — cache_control did not reach the gateway",
			second.promptTokens)
	}
	t.Logf("cached %d/%d prompt tokens (%.0f%%)", second.cacheHitTokens, second.promptTokens,
		100*float64(second.cacheHitTokens)/float64(second.promptTokens))
}
