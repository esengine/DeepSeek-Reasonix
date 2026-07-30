package autoresearch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDirectionFingerprintDistinguishesLongPrefixes: two directions sharing a
// >56-char slug prefix used to collapse to one fingerprint, wrongly counting
// the second as a repeat and inflating StaleCount toward a forced pivot.
func TestDirectionFingerprintDistinguishesLongPrefixes(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	task, err := store.CreateTask("Long prefix fingerprints", CreateOptions{})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	prefix := "Benchmark the desktop frontend markdown rendering pipeline for "

	progress, err := store.RecordDirection(task.ID, Direction{
		Summary:             prefix + "large tables",
		AcceptedEvidenceIDs: []string{"f1"},
		Now:                 time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordDirection first: %v", err)
	}
	if progress.StaleCount != 0 {
		t.Fatalf("first direction stale = %d, want 0", progress.StaleCount)
	}

	progress, err = store.RecordDirection(task.ID, Direction{
		Summary:             prefix + "code blocks",
		AcceptedEvidenceIDs: []string{"f2"},
		Now:                 time.Date(2026, 7, 7, 10, 1, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordDirection second: %v", err)
	}
	// A genuinely different direction with accepted evidence must not be
	// counted as a repeat.
	if progress.StaleCount != 0 {
		t.Fatalf("distinct long-prefix direction treated as repeat: stale = %d, want 0", progress.StaleCount)
	}

	// An exact repeat must still be detected.
	progress, err = store.RecordDirection(task.ID, Direction{
		Summary:             prefix + "code blocks",
		AcceptedEvidenceIDs: []string{"f3"},
		Now:                 time.Date(2026, 7, 7, 10, 2, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordDirection third: %v", err)
	}
	if progress.StaleCount != 1 {
		t.Fatalf("exact repeat not detected: stale = %d, want 1", progress.StaleCount)
	}
}

// TestDirectionFingerprintDistinguishesCJK: directions differing only in CJK
// text slugify to the same string ("task") and used to collide.
func TestDirectionFingerprintDistinguishesCJK(t *testing.T) {
	a := directionFingerprint("评测甲方案的渲染性能")
	b := directionFingerprint("评测乙方案的渲染性能")
	if a == b {
		t.Fatalf("CJK-only-diff directions share a fingerprint: %q", a)
	}
	if directionFingerprint("评测甲方案的渲染性能") != a {
		t.Fatal("fingerprint is not deterministic")
	}
}

// TestDirectionFingerprintBackwardCompatShortASCII: short ASCII summaries keep
// the bare slug so fingerprints recorded by older versions still match.
func TestDirectionFingerprintBackwardCompatShortASCII(t *testing.T) {
	got := directionFingerprint("Profile markdown rendering")
	want := slugify("Profile markdown rendering")
	if got != want {
		t.Fatalf("short ASCII fingerprint = %q, want legacy slug %q", got, want)
	}
}

// TestRecordDirectionMigratesLegacyFingerprint: a directions_tried.json entry
// written by an older version (bare truncated slug) must still match its own
// summary on repeat, not be double-counted as a new direction.
func TestRecordDirectionMigratesLegacyFingerprint(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	task, err := store.CreateTask("Legacy fingerprint migration", CreateOptions{})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	summary := "Benchmark the desktop frontend markdown rendering pipeline for large tables"

	// Simulate a legacy entry: fingerprint from the old bare slugify.
	dirPath := filepath.Join(task.Root, "state", "directions_tried.json")
	legacy := []DirectionTried{{
		Fingerprint:        slugify(summary),
		Summary:            summary,
		FirstSeenIteration: 1,
		LastSeenIteration:  1,
		Count:              1,
	}}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dirPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	progress, err := store.RecordDirection(task.ID, Direction{
		Summary: summary,
		Now:     time.Date(2026, 7, 7, 11, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordDirection: %v", err)
	}
	// Repeat of the legacy direction: stale should increment (repeat + no
	// accepted evidence), and the entry should be migrated, not duplicated.
	if progress.StaleCount != 1 {
		t.Fatalf("legacy repeat not detected: stale = %d, want 1", progress.StaleCount)
	}
	raw, err := os.ReadFile(dirPath)
	if err != nil {
		t.Fatal(err)
	}
	var directions []DirectionTried
	if err := json.Unmarshal(raw, &directions); err != nil {
		t.Fatal(err)
	}
	if len(directions) != 1 {
		t.Fatalf("directions = %+v, want single migrated entry", directions)
	}
	if directions[0].Count != 2 {
		t.Fatalf("migrated count = %d, want 2", directions[0].Count)
	}
	if directions[0].Fingerprint != directionFingerprint(summary) {
		t.Fatalf("entry not migrated to new fingerprint: %q", directions[0].Fingerprint)
	}
}

// TestTailJSONLLinesMatchesFullScan: the tail reader must return exactly the
// same entries as a full scan for every limit, including limits larger than
// the file and files bigger than one read chunk.
func TestTailJSONLLinesMatchesFullScan(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	task, err := store.CreateTask("Tail read equivalence", CreateOptions{})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Write enough heartbeats that the log exceeds one 64KiB chunk.
	long := strings.Repeat("x", 700)
	for i := 0; i < 150; i++ {
		if err := store.AppendHeartbeat(task.ID, Heartbeat{
			Status:    HeartbeatTurnDone,
			Iteration: i + 1,
			Message:   fmt.Sprintf("turn-%03d %s", i, long),
			CreatedAt: time.Date(2026, 7, 7, 12, 0, i%60, 0, time.UTC),
		}); err != nil {
			t.Fatalf("AppendHeartbeat %d: %v", i, err)
		}
	}
	all, err := store.Heartbeats(task.ID, 0)
	if err != nil {
		t.Fatalf("Heartbeats(0): %v", err)
	}
	if len(all) != 150 {
		t.Fatalf("full scan = %d heartbeats, want 150", len(all))
	}
	for _, limit := range []int{1, 3, 149, 150, 500} {
		got, err := store.Heartbeats(task.ID, limit)
		if err != nil {
			t.Fatalf("Heartbeats(%d): %v", limit, err)
		}
		want := all
		if limit < len(all) {
			want = all[len(all)-limit:]
		}
		if len(got) != len(want) {
			t.Fatalf("Heartbeats(%d) = %d entries, want %d", limit, len(got), len(want))
		}
		for i := range got {
			if got[i].Iteration != want[i].Iteration {
				t.Fatalf("Heartbeats(%d)[%d].Iteration = %d, want %d", limit, i, got[i].Iteration, want[i].Iteration)
			}
		}
	}
	// LastHeartbeat must be the newest entry.
	last, ok, err := store.LastHeartbeat(task.ID)
	if err != nil || !ok {
		t.Fatalf("LastHeartbeat: ok=%v err=%v", ok, err)
	}
	if last.Iteration != 150 {
		t.Fatalf("LastHeartbeat iteration = %d, want 150", last.Iteration)
	}
}

// TestFindingsBoundedTailMatchesFullScan mirrors the heartbeat check for the
// findings log, which desktop views read with a limit.
func TestFindingsBoundedTailMatchesFullScan(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	task, err := store.CreateTask("Findings tail equivalence", CreateOptions{})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	for i := 0; i < 25; i++ {
		if err := store.AppendFinding(task.ID, Finding{
			ID:        fmt.Sprintf("f%03d", i),
			Kind:      FindingKindManual,
			Summary:   fmt.Sprintf("finding %03d", i),
			Accepted:  i%2 == 0,
			CreatedAt: time.Date(2026, 7, 7, 13, 0, i%60, 0, time.UTC),
		}); err != nil {
			t.Fatalf("AppendFinding %d: %v", i, err)
		}
	}
	all, err := store.Findings(task.ID, 0)
	if err != nil {
		t.Fatalf("Findings(0): %v", err)
	}
	if len(all) != 25 || all[0].ID != "f024" {
		t.Fatalf("full scan = %d findings first %q, want 25 / f024 (newest first)", len(all), all[0].ID)
	}
	got, err := store.Findings(task.ID, 5)
	if err != nil {
		t.Fatalf("Findings(5): %v", err)
	}
	if len(got) != 5 || got[0].ID != "f024" || got[4].ID != "f020" {
		t.Fatalf("Findings(5) = %+v, want newest five f024..f020", got)
	}
}
