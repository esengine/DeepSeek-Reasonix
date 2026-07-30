package cli

import (
	"strings"
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/provider"
)

// cacheTagCtrl stubs the two SessionAPI reads cacheTag performs; every other
// method panics via the embedded nil interface, which is exactly what we want
// from a focused status-line test.
type cacheTagCtrl struct {
	control.SessionAPI
	last      *provider.Usage
	hit, miss int
}

func (s cacheTagCtrl) LastUsage() *provider.Usage { return s.last }
func (s cacheTagCtrl) SessionCache() (int, int)   { return s.hit, s.miss }

func TestCacheTagHiddenWhenProviderReportsNoCacheFields(t *testing.T) {
	// A provider without prompt-cache support reports prompt tokens but no
	// cache hit/miss fields. Falling back to PromptTokens as the denominator
	// used to paint a bogus "turn hit 0.00%"; the tag must stay empty instead.
	m := chatTUI{ctrl: cacheTagCtrl{last: &provider.Usage{PromptTokens: 1000}}}
	if got := m.cacheTag(); got != "" {
		t.Fatalf("cacheTag with no cache fields = %q, want empty", got)
	}
}

func TestCacheTagShowsRealZeroHit(t *testing.T) {
	// A genuine full miss (provider reports the fields, hit is zero) is
	// informative and must still render.
	m := chatTUI{ctrl: cacheTagCtrl{last: &provider.Usage{PromptTokens: 1000, CacheMissTokens: 1000}}}
	if got := m.cacheTag(); !strings.Contains(got, "0.00%") {
		t.Fatalf("cacheTag with a real full miss = %q, want 0.00%% rendered", got)
	}
}

func TestCacheTagRendersHitRateAndSessionAverage(t *testing.T) {
	m := chatTUI{ctrl: cacheTagCtrl{
		last: &provider.Usage{CacheHitTokens: 80, CacheMissTokens: 20},
		hit:  700, miss: 300,
	}}
	got := m.cacheTag()
	if !strings.Contains(got, "80.00%") {
		t.Fatalf("cacheTag = %q, want turn rate 80.00%%", got)
	}
	if !strings.Contains(got, "70.00%") {
		t.Fatalf("cacheTag = %q, want session average 70.00%%", got)
	}
}
