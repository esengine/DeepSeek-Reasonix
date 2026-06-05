package agent

import (
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// renderUsage drives a Usage event through a fresh TextSink (no renderer) and
// returns what it wrote — the usage line, exercised through the event path.
func renderUsage(u *provider.Usage, p *provider.Pricing, d ...*event.CacheDiagnostics) string {
	var b strings.Builder
	var diag *event.CacheDiagnostics
	if len(d) > 0 {
		diag = d[0]
	}
	NewTextSink(&b, nil, 80).Emit(event.Event{Kind: event.Usage, Usage: u, Pricing: p, CacheDiagnostics: diag})
	return b.String()
}

func TestUsageLine(t *testing.T) {
	u := &provider.Usage{
		PromptTokens:     1000,
		CompletionTokens: 200,
		TotalTokens:      1200,
		CacheHitTokens:   900,
		CacheMissTokens:  100,
	}

	if out := renderUsage(u, nil); !strings.Contains(out, "1200 tok") || !strings.Contains(out, "900 cached / 100 new") {
		t.Errorf("usage line = %q (want 1200 tok and 900 cached / 100 new)", out)
	}

	// With pricing: 900*0.02 + 100*1 + 200*2 = 518 per 1M = 0.000518 -> "¥0.0005".
	if out := renderUsage(u, &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2, Currency: "¥"}); !strings.Contains(out, "¥0.0005") {
		t.Errorf("cost line = %q (want ¥0.0005...)", out)
	}

	// nil or zero usage prints nothing.
	if out := renderUsage(nil, nil) + renderUsage(&provider.Usage{}, nil); out != "" {
		t.Errorf("nil/zero usage should print nothing, got %q", out)
	}
}

// TestUsageLineDerivesMissFromHit covers the OpenAI/MiMo shape where only the
// cached count is reported; the displayed "new" value comes from
// PromptTokens - CacheHitTokens. Verifies the absolute split doesn't show 0.
func TestUsageLineDerivesMissFromHit(t *testing.T) {
	u := &provider.Usage{
		PromptTokens:     3540,
		CompletionTokens: 378,
		TotalTokens:      3918,
		CacheHitTokens:   1133,
		// CacheMissTokens deliberately 0 — provider only reported the hit
	}
	if out := renderUsage(u, nil); !strings.Contains(out, "1133 cached / 2407 new") {
		t.Errorf("usage line = %q (want 1133 cached / 2407 new)", out)
	}
}

func TestUsageLineReportsPrefixChurn(t *testing.T) {
	u := &provider.Usage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110}
	d := &event.CacheDiagnostics{
		PrefixChanged:       true,
		PrefixChangeReasons: []string{"tools", "log_rewrite"},
	}
	if out := renderUsage(u, nil, d); !strings.Contains(out, "cache prefix changed: tools+log_rewrite") {
		t.Errorf("usage line = %q (want cache prefix change reason)", out)
	}
}
