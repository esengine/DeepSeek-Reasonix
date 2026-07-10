// Per-plugin startup latency tracking for MCP servers. Reasonix uses these
// samples to decide whether a chronically slow plugin should be demoted from
// "eager" to background loading for the rest of a session — see Recommend.
//
// Storage is one tiny JSON file per plugin under <cacheDir>/mcp/, written
// atomically (tmpfile + Rename) so a crash mid-write can't corrupt history.
// All errors are best-effort: missing/unreadable files yield "no demote",
// write failures get logged via slog and dropped — startup must not fail
// because telemetry can't persist.
package plugin

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"reasonix/internal/config"
	fileencoding "reasonix/internal/fileutil/encoding"
)

const (
	// statsVersion is the on-disk format version. Bump when StartupStats changes
	// shape incompatibly; older files are then ignored on load (treated as
	// missing) so a new build doesn't crash on a stale schema.
	statsVersion = 1
	// maxSamples bounds the rolling window. 20 is enough to absorb a few flukes
	// while still letting recent regressions dominate p99 / consecutive-fail
	// checks. Older samples drop off the front.
	maxSamples = 20
	// defaultDemoteAfter is the consecutive-over-budget threshold used when the
	// caller passes <= 0. Three in a row is "user has felt it three startups",
	// which matches what the plan calls out as the demote signal.
	defaultDemoteAfter = 3
)

// StartupStats is the on-disk record of recent startup durations for one
// plugin. SamplesMs is oldest→newest (newest appended at the tail); LastSeen
// is the wall-clock time the most recent sample was recorded so a future
// "stale data" pruning step can act on it without re-parsing each sample.
type StartupStats struct {
	Version   int       `json:"version"`
	SamplesMs []int64   `json:"samples_ms"`
	LastSeen  time.Time `json:"last_seen"`
	// Name is the raw (pre-slug) server name that owns these samples. The
	// filename slug is lossy — "foo.bar" and "foo-bar" share one file — so
	// readers verify ownership before trusting samples, or a slow server's
	// history could demote an unrelated healthy one. Empty on files written
	// by older versions; those are adopted by the first writer (additive
	// field, no version bump, so downgraded builds still read the file).
	Name string `json:"name,omitempty"`
}

// Recommendation is the result of inspecting a plugin's recent startup
// history. Demote is the actionable bit boot.go consumes (true → switch this
// plugin to background startup for this session). P99 and Reason are
// descriptive only — Reason is meant to be surfaced to the user as a Notice so a
// sudden demotion isn't silent.
type Recommendation struct {
	Demote bool
	P99    time.Duration
	Reason string
}

// RecordStartup appends one sample (the wall-clock duration of a plugin's
// blocking handshake phase) to that plugin's stats file. The file is created
// on first call; existing samples are kept in a rolling window of maxSamples,
// dropping the oldest when full.
//
// Best-effort: any I/O or marshal failure is logged with slog.Warn and
// returned, but callers are expected to ignore the error — telemetry must
// never block real work. Writes go through a tmpfile + Rename so a partial
// write can't leave the stats file truncated or unparseable.
func RecordStartup(name string, dur time.Duration) error {
	path := statsPath(name)
	if path == "" {
		// No cache dir resolvable on this host — silently skip. This is the
		// same fallback every other persistence helper in the project takes
		// (ArchiveDir/SessionDir return "" and writers no-op).
		return nil
	}

	stats := loadStatsForOwner(name) // missing/corrupt/foreign → fresh zero value
	if stats.Version != statsVersion {
		// Version mismatch: start over rather than try to migrate. The window
		// is small enough that "lose 20 samples" is cheap, and it keeps the
		// migration path trivial as the format evolves.
		stats = StartupStats{Version: statsVersion}
	}
	stats.Name = name

	ms := dur.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	stats.SamplesMs = append(stats.SamplesMs, ms)
	if len(stats.SamplesMs) > maxSamples {
		// Trim from the front: oldest samples leave first.
		stats.SamplesMs = stats.SamplesMs[len(stats.SamplesMs)-maxSamples:]
	}
	stats.LastSeen = time.Now()

	if err := writeStatsAtomic(path, stats); err != nil {
		slog.Warn("plugin: record startup stats failed", "server", name, "err", err)
		return err
	}
	return nil
}

// Recommend inspects the recent samples for name and decides whether the
// plugin should be demoted to "lazy" this session. The rule is simple: demote
// when the last demoteAfter samples all hit or exceed the blocking startup
// budget. Missing/empty stats → no demote (a fresh plugin gets the benefit of
// the doubt and one normal startup attempt).
//
// budget == 0 disables the check (returns no-demote). demoteAfter <= 0 falls
// back to defaultDemoteAfter so callers can pass the config value verbatim
// without sanitising it.
func Recommend(name string, budget time.Duration, demoteAfter int) Recommendation {
	if budget <= 0 {
		return Recommendation{}
	}
	if demoteAfter <= 0 {
		demoteAfter = defaultDemoteAfter
	}

	path := statsPath(name)
	if path == "" {
		return Recommendation{}
	}
	stats := loadStatsForOwner(name)
	if stats.Version != statsVersion || len(stats.SamplesMs) == 0 {
		// Either no history yet or a format we can't read — give the plugin a
		// chance. The cost of one slow start is small compared to wrongly
		// demoting a healthy plugin off stale data.
		return Recommendation{}
	}

	rec := Recommendation{P99: p99(stats.SamplesMs)}
	if len(stats.SamplesMs) < demoteAfter {
		return rec
	}

	threshold := budget.Milliseconds()
	tail := stats.SamplesMs[len(stats.SamplesMs)-demoteAfter:]
	for _, ms := range tail {
		if ms < threshold {
			return rec
		}
	}
	rec.Demote = true
	rec.Reason = fmt.Sprintf(
		"plugin %q has been slow %d startups in a row (last %dms, budget %dms); demoting to background startup this session",
		name, demoteAfter, tail[len(tail)-1], budget.Milliseconds(),
	)
	return rec
}

// statsPath returns the canonical path for one plugin's stats file:
// <config.CacheDir()>/mcp/<slug>-<hash>.stats.json. The slug alone is lossy —
// "foo.bar" and "foo-bar" collapse to one slug — and two colliding servers
// sharing a file would alternately reset each other's window, so neither
// could ever accumulate enough consecutive samples to demote. Hashing the raw
// name into the filename gives every server its own history. The slug portion
// is bounded so the whole component stays under the 255-byte filesystem limit
// even for very long server names — the hash carries the uniqueness, so
// truncating the slug is safe. Returns "" when no cache dir is resolvable,
// which all callers treat as "skip telemetry".
func statsPath(name string) string {
	base := config.CacheDir()
	if base == "" {
		return ""
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	// 255-byte component budget: slug + "-" + 8 hex digits + ".stats.json".
	stem := slug(name)
	if maxStem := 255 - len("-00000000.stats.json"); len(stem) > maxStem {
		stem = stem[:maxStem]
	}
	return filepath.Join(base, "mcp", fmt.Sprintf("%s-%08x.stats.json", stem, h.Sum32()))
}

// legacyStatsPath is the pre-hash location, shared by every server whose name
// collapses to the same slug. Read-only for migration: a named legacy file is
// adopted by its owner; a nameless one (pre-Name builds) has no provable owner
// and is never trusted for demotion or adopted into a new window.
func legacyStatsPath(name string) string {
	base := config.CacheDir()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "mcp", slug(name)+".stats.json")
}

// loadStats reads and decodes a stats file. Any failure (missing file,
// permission denied, malformed JSON) returns a zero StartupStats so callers
// can treat absence and corruption identically. The slog.Warn fires only on
// non-NotExist read errors and on JSON errors — those are surprising enough
// to be worth a trace, but they still must not stop the caller.
func loadStats(path string) StartupStats {
	var s StartupStats
	b, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("plugin: read startup stats failed", "path", path, "err", err)
		}
		return s
	}
	if err := json.Unmarshal(b, &s); err != nil {
		slog.Warn("plugin: parse startup stats failed", "path", path, "err", err)
		return StartupStats{}
	}
	return s
}

// loadStatsForOwner reads name's history from its hashed path, falling back to
// the shared legacy (pre-hash) file for a one-time migration. Legacy files are
// trusted only when their recorded Name matches: a nameless legacy file
// (written before ownership tracking) is shared by every server whose name
// collapses to the same slug, so its samples cannot be attributed and must not
// seed anyone's window or demote anyone.
func loadStatsForOwner(name string) StartupStats {
	if s := loadStats(statsPath(name)); s.Version != 0 || len(s.SamplesMs) > 0 {
		if s.Name == "" || s.Name == name {
			return s
		}
		return StartupStats{}
	}
	legacy := legacyStatsPath(name)
	if legacy == "" {
		return StartupStats{}
	}
	if s := loadStats(legacy); s.Name == name {
		return s
	}
	return StartupStats{}
}

// writeStatsAtomic serialises s and writes it via tmpfile + os.Rename so that
// concurrent readers see either the old content or the new one, never a
// half-written file. Mirrors desktop/sessions.go:40-64.
func writeStatsAtomic(path string, s StartupStats) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".stats.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

// p99 returns the 99th-percentile sample as a Duration. With small windows
// (n ≤ 20) "p99" collapses to "the slowest sample we have"; the value is
// purely informational, surfaced in Recommendation for UI/notice text.
func p99(samples []int64) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	sorted := append([]int64(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	// Use ceil(0.99 * n) - 1 so that for n=1..100 we always pick the last
	// element; for larger n it's the index at the 99% boundary.
	idx := int(float64(len(sorted))*0.99+0.9999999) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return time.Duration(sorted[idx]) * time.Millisecond
}
