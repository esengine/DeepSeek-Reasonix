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

// TestStatsSlugCollisionDoesNotCrossDemote: "foo.bar" and "foo-bar" collapse
// to one slug. Each server now gets its own hash-suffixed file, so a slow
// server's samples must not demote the other, and interleaved startups must
// not reset each other's windows (the old shared-file ownership check made
// each writer wipe the other's history, so neither could ever accumulate
// enough consecutive samples to demote).
func TestStatsSlugCollisionDoesNotCrossDemote(t *testing.T) {
	withTempCache(t)

	// Interleave a chronically slow server with a healthy one sharing the slug.
	for i := 0; i < defaultDemoteAfter; i++ {
		if err := RecordStartup("foo.bar", 5*time.Second); err != nil {
			t.Fatalf("RecordStartup foo.bar: %v", err)
		}
		if err := RecordStartup("foo-bar", 10*time.Millisecond); err != nil {
			t.Fatalf("RecordStartup foo-bar: %v", err)
		}
	}
	// Both windows must have accumulated independently despite interleaving.
	slow := readStats(t, "foo.bar")
	fast := readStats(t, "foo-bar")
	if len(slow.SamplesMs) != defaultDemoteAfter || slow.Name != "foo.bar" {
		t.Fatalf("foo.bar window = %+v, want %d samples owned by foo.bar", slow, defaultDemoteAfter)
	}
	if len(fast.SamplesMs) != defaultDemoteAfter || fast.Name != "foo-bar" {
		t.Fatalf("foo-bar window = %+v, want %d samples owned by foo-bar", fast, defaultDemoteAfter)
	}
	// The slow server demotes on its own history; the healthy one does not.
	if rec := Recommend("foo.bar", time.Second, 0); !rec.Demote {
		t.Fatalf("foo.bar should demote on its own history: %+v", rec)
	}
	if rec := Recommend("foo-bar", time.Second, 0); rec.Demote {
		t.Fatalf("foo-bar demoted off foo.bar's samples: %+v", rec)
	}
}

// TestStatsLegacyNamedFileMigrated: a pre-hash stats file whose recorded Name
// matches is this server's own history — Recommend keeps working from it and
// the next write carries it into the hashed path.
func TestStatsLegacyNamedFileMigrated(t *testing.T) {
	withTempCache(t)

	legacy := StartupStats{Version: statsVersion, Name: "legacy", LastSeen: time.Now()}
	for i := 0; i < 3; i++ {
		legacy.SamplesMs = append(legacy.SamplesMs, 100)
	}
	if err := writeStatsAtomic(legacyStatsPath("legacy"), legacy); err != nil {
		t.Fatalf("write legacy stats: %v", err)
	}

	if rec := Recommend("legacy", time.Second, 0); rec.P99 == 0 {
		t.Fatalf("legacy named stats not readable: %+v", rec)
	}
	if err := RecordStartup("legacy", 100*time.Millisecond); err != nil {
		t.Fatalf("RecordStartup after legacy: %v", err)
	}
	s := readStats(t, "legacy")
	if s.Name != "legacy" || len(s.SamplesMs) != 4 {
		t.Fatalf("legacy migration = %+v, want name kept and 4 samples", s)
	}
}

// TestStatsLegacyNamelessCollisionNotTrusted: a pre-ownership legacy file has
// no Name, so it is shared by every server whose name collapses to the slug
// and its samples cannot be attributed. It must not demote anyone and must
// not seed a new window.
func TestStatsLegacyNamelessCollisionNotTrusted(t *testing.T) {
	withTempCache(t)

	nameless := StartupStats{Version: statsVersion, LastSeen: time.Now()}
	for i := 0; i < defaultDemoteAfter; i++ {
		nameless.SamplesMs = append(nameless.SamplesMs, 5000) // over budget
	}
	if err := writeStatsAtomic(legacyStatsPath("foo.bar"), nameless); err != nil {
		t.Fatalf("write nameless legacy stats: %v", err)
	}

	// Neither colliding server may be demoted off unattributable samples.
	if rec := Recommend("foo.bar", time.Second, 0); rec.Demote {
		t.Fatalf("foo.bar demoted off nameless legacy samples: %+v", rec)
	}
	if rec := Recommend("foo-bar", time.Second, 0); rec.Demote {
		t.Fatalf("foo-bar demoted off nameless legacy samples: %+v", rec)
	}
	// A new write starts a fresh window instead of inheriting the samples.
	if err := RecordStartup("foo.bar", 10*time.Millisecond); err != nil {
		t.Fatalf("RecordStartup: %v", err)
	}
	s := readStats(t, "foo.bar")
	if s.Name != "foo.bar" || len(s.SamplesMs) != 1 {
		t.Fatalf("window after nameless legacy = %+v, want fresh single-sample window", s)
	}
}

// TestStatsLegacyFileWithoutNameAdopted: a nameless file at the hashed path is
// unambiguous — the hash pins it to exactly one server name — so its samples
// stay usable and the next write adopts it without dropping history. (Nameless
// files at the shared legacy path are the untrusted case, covered above.)
func TestStatsLegacyFileWithoutNameAdopted(t *testing.T) {
	withTempCache(t)

	for i := 0; i < 3; i++ {
		if err := RecordStartup("legacy", 100*time.Millisecond); err != nil {
			t.Fatalf("RecordStartup: %v", err)
		}
	}
	// Simulate a legacy file: strip the Name field.
	path := statsPath("legacy")
	s := readStats(t, "legacy")
	s.Name = ""
	if err := writeStatsAtomic(path, s); err != nil {
		t.Fatalf("write legacy stats: %v", err)
	}

	// Recommend still reads the legacy file (no ownership check possible).
	if rec := Recommend("legacy", time.Second, 0); rec.P99 == 0 {
		t.Fatalf("legacy stats not readable: %+v", rec)
	}
	// The next write adopts the file without dropping history.
	if err := RecordStartup("legacy", 100*time.Millisecond); err != nil {
		t.Fatalf("RecordStartup after legacy: %v", err)
	}
	s = readStats(t, "legacy")
	if s.Name != "legacy" || len(s.SamplesMs) != 4 {
		t.Fatalf("legacy adoption = %+v, want name set and 4 samples kept", s)
	}
}

// TestStatsPathBoundedForLongNames: the hash suffix added for collision
// safety must not push a previously-writable long slug past the 255-byte
// filename component limit (slugs of 236-244 bytes could write
// "<slug>.stats.json" before, but "<slug>-<hash>.stats.json" would exceed it).
func TestStatsPathBoundedForLongNames(t *testing.T) {
	withTempCache(t)

	long := strings.Repeat("a", 240)
	p := statsPath(long)
	if p == "" {
		t.Fatal("statsPath returned empty")
	}
	if base := filepath.Base(p); len(base) > 255 {
		t.Fatalf("component = %d bytes, exceeds 255 limit: %q", len(base), base)
	}
	// The bounded path must still be writable and readable end-to-end.
	if err := RecordStartup(long, 100*time.Millisecond); err != nil {
		t.Fatalf("RecordStartup long name: %v", err)
	}
	s := readStats(t, long)
	if s.Name != long || len(s.SamplesMs) != 1 {
		t.Fatalf("long-name stats = %+v, want one owned sample", s)
	}
	// Distinct long names sharing the truncated stem must keep distinct files.
	other := strings.Repeat("a", 240) + "b"
	if statsPath(other) == p {
		t.Fatalf("distinct long names collapsed to one stats path: %q", p)
	}
}
