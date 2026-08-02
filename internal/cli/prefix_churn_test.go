package cli

import (
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// TestPrefixChurnTracking pins the cache-prefix reset diagnostics: Usage
// events with PrefixChanged accumulate per-cause counts, and the /status tag
// renders them in stable order.
func TestPrefixChurnTracking(t *testing.T) {
	m := newTestChatTUI()
	if tag := m.prefixChurnTag(); tag != "" {
		t.Fatalf("no-churn prefix tag = %q, want empty", tag)
	}

	m.ingestEvent(event.Event{
		Kind: event.Usage,
		Usage: &provider.Usage{
			PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150,
		},
		CacheDiagnostics: &event.CacheDiagnostics{
			PrefixChanged:       true,
			PrefixChangeReasons: []string{"tools", "tools"},
		},
	})
	if m.prefixChurns["tools"] != 2 {
		t.Fatalf("tools churn count = %d, want 2", m.prefixChurns["tools"])
	}

	m.ingestEvent(event.Event{
		Kind: event.Usage,
		Usage: &provider.Usage{
			PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150,
		},
		CacheDiagnostics: &event.CacheDiagnostics{
			PrefixChanged:       true,
			PrefixChangeReasons: []string{"log_rewrite"},
		},
	})
	if m.prefixChurns["log_rewrite"] != 1 {
		t.Fatalf("log_rewrite churn count = %d, want 1", m.prefixChurns["log_rewrite"])
	}

	tag := strings.TrimSpace(strings.ReplaceAll(m.prefixChurnTag(), "\x1b[", ""))
	if !strings.Contains(tag, "3 resets") || !strings.Contains(tag, "log_rewrite×1") || !strings.Contains(tag, "tools×2") {
		t.Fatalf("prefix churn tag = %q, want 3 resets with per-cause counts", tag)
	}

	// A non-churning usage event leaves the counters untouched.
	m.ingestEvent(event.Event{
		Kind:  event.Usage,
		Usage: &provider.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
	})
	if m.prefixChurns["tools"] != 2 {
		t.Fatalf("non-churn usage changed the tools count: %d", m.prefixChurns["tools"])
	}
}
