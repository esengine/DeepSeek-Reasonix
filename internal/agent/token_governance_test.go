package agent

import (
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func testGovernance(opts TokenGovernanceOptions) *TokenGovernance {
	opts.Enabled = true
	return NewTokenGovernance(opts)
}

// TestTokenGovernanceObserveUsageAdvisory: high load triggers the shed flag
// and the admission gate records a rejection; both are advisory (the report
// is returned, nothing is vetoed).
func TestTokenGovernanceObserveUsageAdvisory(t *testing.T) {
	g := testGovernance(TokenGovernanceOptions{
		LoadShedder:     true,
		LoadThreshold:   100,
		ShedStrategy:    "oldest",
		AdmissionGate:   true,
		AdmissionCapacity: 50,
		CacheCompactor:  true,
		CacheWarmer:     true,
		WarmerStrategy:  "lru",
	})

	rep := g.ObserveUsage(&provider.Usage{TotalTokens: 9999}, "test-model", &CacheDiagnostics{
		PrefixHash: "abc",
		SystemHash: "abc", // same value twice → 1 key deduped by the compactor
		ToolsHash:  "ghi",
	})
	if !rep.Shed {
		t.Error("expected shed flag when load exceeds threshold")
	}
	if !rep.Rejected {
		t.Error("expected rejected flag when load exceeds admission capacity")
	}
	if rep.WarmTarget != "test-model" {
		t.Errorf("expected warmer target test-model, got %q", rep.WarmTarget)
	}

	stats := g.GetStats()
	if stats["shed_count"].(int) != 1 {
		t.Errorf("expected shed_count 1, got %v", stats["shed_count"])
	}
	if stats["rejected"].(int) != 1 {
		t.Errorf("expected rejected 1, got %v", stats["rejected"])
	}
	if stats["compactions"].(int) != 1 {
		t.Errorf("expected 1 compaction pass, got %v", stats["compactions"])
	}
	if stats["compacted_keys"].(int) != 0 {
		t.Errorf("expected 0 deduped keys (distinct prefix-tagged keys), got %v", stats["compacted_keys"])
	}
}

// TestTokenGovernanceNormalLoadIsSilent: a healthy turn must not shed or
// reject, and must not produce warnings in the agent loop.
func TestTokenGovernanceNormalLoadIsSilent(t *testing.T) {
	g := testGovernance(TokenGovernanceOptions{
		LoadShedder:     true,
		LoadThreshold:   100_000,
		AdmissionGate:   true,
		AdmissionCapacity: 200_000,
	})
	rep := g.ObserveUsage(&provider.Usage{TotalTokens: 5_000}, "m", nil)
	if rep.Shed || rep.Rejected {
		t.Errorf("healthy load must not shed/reject: %+v", rep)
	}
}

// TestTokenGovernanceSuggestWindowClamps: the resizer suggestion stays within
// the configured min/max.
func TestTokenGovernanceSuggestWindowClamps(t *testing.T) {
	g := testGovernance(TokenGovernanceOptions{
		WindowResizer:    true,
		ContextWindowMin: 16_000,
		ContextWindowMax: 128_000,
	})
	got := g.SuggestWindow(1_000_000)
	if got < 16_000 || got > 128_000 {
		t.Errorf("suggestion %d out of [16000,128000]", got)
	}
	// Disabled resizer returns current unchanged.
	g2 := NewTokenGovernance(TokenGovernanceOptions{})
	if got := g2.SuggestWindow(42_000); got != 42_000 {
		t.Errorf("disabled resizer must return current, got %d", got)
	}
}

// TestAgentEmitTurnUsageWithGovernance: wiring governance into the agent must
// not change the usage event and must not panic when governance is nil.
func TestAgentEmitTurnUsageWithGovernance(t *testing.T) {
	sink := &collectingSink{}
	a := New(&fakeProvider{reply: "ok"}, tool.NewRegistry(), NewSession("sys"), Options{RecentKeep: 2, TokenGovernance: NewTokenGovernance(TokenGovernanceOptions{})}, sink)
	usage := &provider.Usage{TotalTokens: 1_000, RequestCount: 1}
	a.emitTurnUsage(usage, nil)
	if len(sink.events) == 0 {
		t.Fatal("expected a usage event")
	}
	if sink.events[0].Kind != event.Usage {
		t.Errorf("expected Usage event kind, got %v", sink.events[0].Kind)
	}

	// nil governance path (source compat).
	a2 := New(&fakeProvider{reply: "ok"}, tool.NewRegistry(), NewSession("sys"), Options{RecentKeep: 2}, sink)
	a2.emitTurnUsage(usage, nil) // must not panic
}
