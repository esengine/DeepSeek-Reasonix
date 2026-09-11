package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func usageEvent(source string, prompt, completion int) event.Event {
	return event.Event{
		Kind:        event.Usage,
		UsageSource: source,
		Usage:       &provider.Usage{PromptTokens: prompt, CompletionTokens: completion, CacheMissTokens: prompt},
		Pricing:     &provider.Pricing{Input: 1, Output: 2, CacheHit: 0.1, Currency: "$"},
	}
}

func usageEventWithCacheReason(reason string) event.Event {
	e := usageEvent(event.UsageSourceSubagent, 10, 1)
	e.CacheDiagnostics = &event.CacheDiagnostics{PrefixChangeReasons: []string{reason}}
	return e
}

func TestSnapshotDeepCopiesPrefixChangeReasons(t *testing.T) {
	s := &metricsSink{inner: event.Discard}
	s.Emit(usageEventWithCacheReason("compact_auto"))

	snapshot := s.Snapshot()
	s.Emit(usageEventWithCacheReason("snip"))

	if snapshot.PrefixChangeReasonCounts["compact_auto"] != 1 {
		t.Fatalf("snapshot compact_auto = %d, want 1", snapshot.PrefixChangeReasonCounts["compact_auto"])
	}
	if _, changed := snapshot.PrefixChangeReasonCounts["snip"]; changed {
		t.Fatalf("snapshot changed after return: %v", snapshot.PrefixChangeReasonCounts)
	}
}

// A killed agent writes no final record. Everything it did before the kill is
// only recoverable if snapshots landed on disk while it ran.
func TestSnapshotSurvivesWithoutAFinalWrite(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "metrics.json")
	now := time.Unix(0, 0)
	s := &metricsSink{
		inner:         event.Discard,
		partialPath:   partialMetricsPath(final),
		snapshotEvery: time.Second,
		clock:         func() time.Time { return now },
	}

	s.Emit(usageEvent(event.UsageSourceExecutor, 1000, 10))
	now = now.Add(2 * time.Second)
	s.Emit(usageEvent(event.UsageSourceExecutor, 500, 5))

	raw, err := os.ReadFile(partialMetricsPath(final))
	if err != nil {
		t.Fatalf("no snapshot on disk: %v", err)
	}
	var got RunMetrics
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("snapshot is not parseable JSON: %v", err)
	}
	if got.Complete {
		t.Error("a snapshot must never claim to be complete")
	}
	if got.PromptTokens == 0 || got.Steps == 0 {
		t.Errorf("snapshot lost the accounting it exists to preserve: %+v", got)
	}
	if _, err := os.Stat(final); !os.IsNotExist(err) {
		t.Error("no final record should exist for a run that never finished")
	}
}

// Snapshots are throttled: a run makes thousands of events and must not make
// thousands of disk writes.
func TestSnapshotsAreThrottled(t *testing.T) {
	dir := t.TempDir()
	now := time.Unix(0, 0)
	s := &metricsSink{
		inner:         event.Discard,
		partialPath:   filepath.Join(dir, "m.json.partial"),
		snapshotEvery: time.Minute,
		clock:         func() time.Time { return now },
	}
	for range 50 {
		s.Emit(usageEvent(event.UsageSourceExecutor, 10, 1))
	}
	raw, err := os.ReadFile(s.partialPath)
	if err != nil {
		t.Fatalf("first snapshot should still be written: %v", err)
	}
	var got RunMetrics
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Steps != 1 {
		t.Fatalf("snapshot steps = %d, want 1 — later events must not have rewritten within the window", got.Steps)
	}
}

// A completed run must leave exactly one readable record, or a reader could
// count the run twice.
func TestFinalWriteRetiresTheSnapshot(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "metrics.json")
	now := time.Unix(0, 0)
	s := &metricsSink{
		inner:         event.Discard,
		partialPath:   partialMetricsPath(final),
		snapshotEvery: time.Millisecond,
		clock:         func() time.Time { now = now.Add(time.Second); return now },
	}
	s.Emit(usageEvent(event.UsageSourceExecutor, 100, 10))
	if _, err := os.Stat(partialMetricsPath(final)); err != nil {
		t.Fatalf("expected a snapshot before the final write: %v", err)
	}

	if err := writeMetrics(final, s.Snapshot()); err != nil {
		t.Fatalf("writeMetrics: %v", err)
	}

	raw, err := os.ReadFile(final)
	if err != nil {
		t.Fatalf("final record missing: %v", err)
	}
	var got RunMetrics
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.Complete {
		t.Error("the final record must be marked complete")
	}
	if _, err := os.Stat(partialMetricsPath(final)); !os.IsNotExist(err) {
		t.Error("the snapshot must be retired so it cannot be double-counted")
	}
}

// Steps counts every billed call; the breakdown is what makes a total above
// max_steps explicable. An unrecognised origin must survive rather than vanish
// from a total that is meant to reconcile.
func TestUsageBySourceReconcilesWithTheTotal(t *testing.T) {
	s := &metricsSink{inner: event.Discard}
	s.Emit(usageEvent(event.UsageSourceExecutor, 100, 10))
	s.Emit(usageEvent(event.UsageSourceSubagent, 200, 20))
	s.Emit(usageEvent(event.UsageSourceCompaction, 300, 30))
	s.Emit(usageEvent("some-future-origin", 400, 40))
	s.Emit(usageEvent("", 500, 50)) // empty means executor, per the Usage contract

	m := s.Snapshot()
	if len(m.UsageBySource) != 4 {
		t.Fatalf("sources = %v, want executor/subagent/compaction/some-future-origin", m.UsageBySource)
	}
	if got := m.UsageBySource[event.UsageSourceExecutor].Calls; got != 2 {
		t.Errorf("executor calls = %d, want 2 (an empty source is the executor)", got)
	}
	if _, ok := m.UsageBySource["some-future-origin"]; !ok {
		t.Error("an unknown origin must be kept, not dropped")
	}

	var calls, prompt int
	for _, u := range m.UsageBySource {
		calls += u.Calls
		prompt += u.PromptTokens
	}
	if calls != m.Steps {
		t.Errorf("source calls sum to %d but Steps is %d — the breakdown must reconcile", calls, m.Steps)
	}
	if prompt != m.PromptTokens {
		t.Errorf("source prompt tokens sum to %d but total is %d", prompt, m.PromptTokens)
	}
}

// Background jobs emit while the run command assembles the final record.
// Run with -race.
func TestConcurrentEmitAndSnapshotAreRaceFree(t *testing.T) {
	dir := t.TempDir()
	s := &metricsSink{
		inner:         event.Discard,
		partialPath:   filepath.Join(dir, "m.json.partial"),
		snapshotEvery: time.Millisecond,
	}
	const emitters, each = 8, 50

	var wg sync.WaitGroup
	for range emitters {
		wg.Go(func() {
			for range each {
				s.Emit(usageEventWithCacheReason("compact_auto"))
				s.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{Name: "bash"}})
			}
		})
	}
	wg.Go(func() {
		for range 200 {
			if _, err := json.Marshal(s.Snapshot()); err != nil {
				t.Errorf("marshal snapshot: %v", err)
				return
			}
		}
	})
	wg.Wait()

	m := s.Snapshot()
	if m.Steps != emitters*each {
		t.Errorf("steps = %d, want %d — concurrent emission lost counts", m.Steps, emitters*each)
	}
	if m.ToolCalls != emitters*each {
		t.Errorf("tool calls = %d, want %d", m.ToolCalls, emitters*each)
	}
}
