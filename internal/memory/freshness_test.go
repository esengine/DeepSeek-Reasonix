package memory

import (
	"strings"
	"testing"
	"time"
)

var freshnessNow = time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

func agedMemory(age time.Duration) Memory {
	return Memory{Type: TypeProject, UpdatedAt: freshnessNow.Add(-age)}
}

func TestVolatilityOverridesTypeWindows(t *testing.T) {
	old := agedMemory(60 * 24 * time.Hour) // past project fresh (30d), inside current (180d)
	if got := FreshnessFor(old, freshnessNow); got != FreshnessCurrent {
		t.Fatalf("type default: 60d project fact = %q, want current", got)
	}

	volatile := old
	volatile.Volatility = VolatilityVolatile
	if got := FreshnessFor(volatile, freshnessNow); got != FreshnessStale {
		t.Fatalf("volatile 60d fact = %q, want stale (7/30d windows)", got)
	}

	evergreen := agedMemory(3 * 365 * 24 * time.Hour)
	evergreen.Volatility = VolatilityEvergreen
	if got := FreshnessFor(evergreen, freshnessNow); got != FreshnessFresh {
		t.Fatalf("evergreen 3y fact = %q, want fresh forever", got)
	}
}

func TestExpiryIsAHardBoundaryExcludedFromRecall(t *testing.T) {
	store := recallTestStore(t)
	expired := Memory{
		ID: "mem-branch", Name: "release-branch", Title: "Current release branch",
		Description: "release/1.21 is the current release branch", Type: TypeProject,
		Scope: FactScopeProject, Body: "Target release/1.21 for hotfixes.",
	}
	expired.UpdatedAt = freshnessNow.Add(-24 * time.Hour)
	expired.ExpiresAt = freshnessNow.Add(-time.Hour)
	recallTestWrite(t, store.Dir, expired)

	if got := FreshnessFor(expired, freshnessNow); got != FreshnessExpired {
		t.Fatalf("past expires_at = %q, want expired", got)
	}
	result := AutoRecall(store, "hotfix 应该打到 release branch release/1.21 吗", RecallOptions{Now: freshnessNow})
	if len(result.Hits) != 0 {
		t.Fatalf("expired facts must never be auto-recalled: %+v", result.Hits)
	}
	// Explicit search still finds it — expiry hides nothing from management.
	hits, err := searchMemories(t.Context(), store, "release branch", "", "", 8)
	if err != nil || len(hits) != 1 {
		t.Fatalf("explicit search should still see expired facts, got %v %v", hits, err)
	}
}

func TestVerificationRenewsTheFreshnessClock(t *testing.T) {
	m := agedMemory(200 * 24 * time.Hour) // past project current window: stale
	if got := FreshnessFor(m, freshnessNow); got != FreshnessStale {
		t.Fatalf("200d project fact = %q, want stale", got)
	}
	m.LastVerifiedAt = freshnessNow.Add(-24 * time.Hour)
	if got := FreshnessFor(m, freshnessNow); got != FreshnessFresh {
		t.Fatalf("verified yesterday = %q, want fresh", got)
	}
}

func TestSaveRoundTripsVolatilityExpiryAndClearExpiry(t *testing.T) {
	store := recallTestStore(t)
	saved, err := store.SaveWithOptions(Memory{
		Name: "windows-port", Description: "current weekly focus",
		Volatility: VolatilityVolatile, ExpiresAt: freshnessNow.Add(30 * 24 * time.Hour),
		Body: "User is doing the Windows port this week.",
	}, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	loaded, _ := store.Read(saved.Memory.ID)
	if NormalizeVolatility(string(loaded.Volatility)) != VolatilityVolatile || loaded.ExpiresAt.IsZero() {
		t.Fatalf("volatility/expiry lost in round trip: %+v", loaded)
	}

	// An update omitting both preserves them.
	if _, err := store.SaveWithOptions(Memory{ID: saved.Memory.ID, Description: "current weekly focus v2", Body: "Still on the port."}, SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	kept, _ := store.Read(saved.Memory.ID)
	if NormalizeVolatility(string(kept.Volatility)) != VolatilityVolatile || kept.ExpiresAt.IsZero() {
		t.Fatalf("update must inherit volatility/expiry: %+v", kept)
	}

	// ClearExpiry drops the boundary without touching volatility.
	if _, err := store.SaveWithOptions(Memory{ID: saved.Memory.ID, Description: "current weekly focus v3", Body: "Port done."}, SaveOptions{ClearExpiry: true}); err != nil {
		t.Fatal(err)
	}
	cleared, _ := store.Read(saved.Memory.ID)
	if !cleared.ExpiresAt.IsZero() || NormalizeVolatility(string(cleared.Volatility)) != VolatilityVolatile {
		t.Fatalf("ClearExpiry must drop only the expiry: %+v", cleared)
	}
}

func TestParseExpiryFormats(t *testing.T) {
	if _, clear, err := parseExpiry("never"); err != nil || !clear {
		t.Fatalf("never should clear, got clear=%v err=%v", clear, err)
	}
	when, _, err := parseExpiry("2026-09-01")
	if err != nil || when.IsZero() {
		t.Fatalf("bare date rejected: %v", err)
	}
	if _, _, err := parseExpiry("下个月"); err == nil || !strings.Contains(err.Error(), "RFC3339") {
		t.Fatalf("invalid expiry must error with format hint, got %v", err)
	}
}
