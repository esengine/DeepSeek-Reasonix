package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withTempCache redirects config.CacheDir() at t.TempDir for the duration of a
// test (without it the tests write the real user cache).
// Returns the directory that will hold the cache subtree so callers can assert
// paths inside it.
func withTempCache(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("REASONIX_CACHE_HOME", dir)
	return dir
}

// readStats reads the on-disk stats file for name directly (not through the
// public API) so tests can assert raw file contents — what we wrote and in
// what order.
func readStats(t *testing.T, name string) StartupStats {
	t.Helper()
	path := statsPath(name)
	if path == "" {
		t.Fatal("statsPath returned empty")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read stats %s: %v", path, err)
	}
	var s StartupStats
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("unmarshal stats: %v", err)
	}
	return s
}

func TestRecordStartupAppends(t *testing.T) {
	withTempCache(t)

	durs := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 300 * time.Millisecond}
	for _, d := range durs {
		if err := RecordStartup("foo", d); err != nil {
			t.Fatalf("RecordStartup: %v", err)
		}
	}

	s := readStats(t, "foo")
	if s.Version != statsVersion {
		t.Fatalf("Version = %d, want %d", s.Version, statsVersion)
	}
	if got := len(s.SamplesMs); got != 3 {
		t.Fatalf("samples = %d, want 3", got)
	}
	// File is written oldest→newest (newest at the tail).
	want := []int64{100, 200, 300}
	for i, w := range want {
		if s.SamplesMs[i] != w {
			t.Fatalf("SamplesMs[%d] = %d, want %d (full=%v)", i, s.SamplesMs[i], w, s.SamplesMs)
		}
	}
	if s.LastSeen.IsZero() {
		t.Fatal("LastSeen should be set after a record")
	}
}

func TestRecordStartupCapsAtWindow(t *testing.T) {
	withTempCache(t)

	const total = 25
	for i := 0; i < total; i++ {
		// Use distinct values so we can verify the *oldest* ones were dropped.
		if err := RecordStartup("bar", time.Duration(i+1)*time.Millisecond); err != nil {
			t.Fatalf("RecordStartup #%d: %v", i, err)
		}
	}

	s := readStats(t, "bar")
	if got := len(s.SamplesMs); got != maxSamples {
		t.Fatalf("samples = %d, want %d", got, maxSamples)
	}
	// First retained sample should be #6 (1..5 dropped); last should be #25.
	wantFirst := int64(total - maxSamples + 1) // 6
	if s.SamplesMs[0] != wantFirst {
		t.Fatalf("SamplesMs[0] = %d, want %d", s.SamplesMs[0], wantFirst)
	}
	if s.SamplesMs[len(s.SamplesMs)-1] != int64(total) {
		t.Fatalf("SamplesMs[last] = %d, want %d", s.SamplesMs[len(s.SamplesMs)-1], total)
	}
}

func TestRecommendBelowBudgetNoDemote(t *testing.T) {
	withTempCache(t)

	budget := 1 * time.Second
	// All samples comfortably under budget*2 == 2s.
	for _, ms := range []int64{500, 800, 1200, 1500, 900} {
		if err := RecordStartup("ok-plugin", time.Duration(ms)*time.Millisecond); err != nil {
			t.Fatalf("RecordStartup: %v", err)
		}
	}

	got := Recommend("ok-plugin", budget, 3)
	if got.Demote {
		t.Fatalf("Demote = true, want false (reason=%q)", got.Reason)
	}
}

func TestRecommendAboveBudgetDemotes(t *testing.T) {
	withTempCache(t)

	budget := 1 * time.Second
	// First sample is fast (would block a naive "ever-slow" check), then 3
	// consecutive samples above budget*2 == 2s. The plan says demote on
	// "last N consecutive over-budget", so this should trip.
	for _, ms := range []int64{500, 2500, 3000, 4000} {
		if err := RecordStartup("slow-plugin", time.Duration(ms)*time.Millisecond); err != nil {
			t.Fatalf("RecordStartup: %v", err)
		}
	}

	got := Recommend("slow-plugin", budget, 3)
	if !got.Demote {
		t.Fatalf("Demote = false, want true (samples=%+v)", readStats(t, "slow-plugin").SamplesMs)
	}
	if got.Reason == "" {
		t.Fatal("Reason should be non-empty when demoting")
	}
	if got.P99 <= 0 {
		t.Fatalf("P99 = %v, want > 0", got.P99)
	}
}

func TestRecommendAtBudgetDemotes(t *testing.T) {
	withTempCache(t)

	budget := 5 * time.Second
	for i := 0; i < 3; i++ {
		if err := RecordStartup("timeout-plugin", budget); err != nil {
			t.Fatalf("RecordStartup #%d: %v", i, err)
		}
	}

	got := Recommend("timeout-plugin", budget, 3)
	if !got.Demote {
		t.Fatalf("Demote = false, want true for repeated budget hits (samples=%+v)", readStats(t, "timeout-plugin").SamplesMs)
	}
}

func TestRecommendMissingStatsNoDemote(t *testing.T) {
	withTempCache(t)

	// Never recorded anything → should not demote a brand-new plugin.
	got := Recommend("never-seen", 1*time.Second, 3)
	if got.Demote {
		t.Fatalf("Demote = true on missing stats, want false (reason=%q)", got.Reason)
	}
	if got.P99 != 0 {
		t.Fatalf("P99 = %v on missing stats, want 0", got.P99)
	}
}

func TestRecommendOldFailuresFadeOut(t *testing.T) {
	withTempCache(t)

	budget := 1 * time.Second
	// Early samples were terrible (well above budget*2 == 2s), but the most
	// recent three are quick — rolling window must let the plugin recover.
	for _, ms := range []int64{5000, 6000, 7000, 8000, 500, 600, 700} {
		if err := RecordStartup("recovered", time.Duration(ms)*time.Millisecond); err != nil {
			t.Fatalf("RecordStartup: %v", err)
		}
	}

	got := Recommend("recovered", budget, 3)
	if got.Demote {
		t.Fatalf("Demote = true after recovery, want false (samples=%+v, reason=%q)",
			readStats(t, "recovered").SamplesMs, got.Reason)
	}
}

// TestStatsPathLayout pins the on-disk location so Phase 4's integration code
// can rely on it. We assert the path sits under config.CacheDir()/mcp and that
// the slug strips raw separators — exact slug rule lives in cache.go.
func TestStatsPathLayout(t *testing.T) {
	root := withTempCache(t)

	p := statsPath("server/with spaces")
	if p == "" {
		t.Fatal("statsPath returned empty")
	}
	wantParent := filepath.Join(root, "mcp")
	if got := filepath.Dir(p); got != wantParent {
		t.Fatalf("parent = %q, want %q", got, wantParent)
	}
	base := filepath.Base(p)
	if filepath.Ext(base) != ".json" {
		t.Fatalf("ext = %q, want .json", filepath.Ext(base))
	}
	if !strings.HasSuffix(base, ".stats.json") {
		t.Fatalf("base = %q, want .stats.json suffix", base)
	}
	if containsAny(base, "/ ") {
		t.Fatalf("slug %q contains raw separators", base)
	}
}

func containsAny(s, chars string) bool {
	for _, c := range chars {
		for _, r := range s {
			if r == c {
				return true
			}
		}
	}
	return false
}
