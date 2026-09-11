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
