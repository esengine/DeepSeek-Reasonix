package control

import (
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/usagereport"
)

func usageEvent(source string, prompt, hit, miss int) event.Event {
	return event.Event{
		Kind:        event.Usage,
		UsageSource: source,
		Usage: &provider.Usage{
			PromptTokens:    prompt,
			CacheHitTokens:  hit,
			CacheMissTokens: miss,
			RequestCount:    1,
		},
	}
}

// The cold start is the executor's first request and nothing else: the later
// ones reuse the prefix it paid for, which is the whole reason it is reported
// on its own.
func TestUsageTallyRecordsOnlyTheFirstExecutorRequest(t *testing.T) {
	tally := newUsageTally()
	tally.observe(usageEvent(event.UsageSourceExecutor, 9000, 400, 8600))
	tally.observe(usageEvent(event.UsageSourceExecutor, 9500, 9000, 500))

	cold := tally.usage.ColdStart
	if cold == nil {
		t.Fatal("no cold start recorded for a session that made an executor request")
	}
	if cold.PromptTokens != 9000 || cold.CacheHitTokens != 400 || cold.CacheMissTokens != 8600 {
		t.Fatalf("cold start = %+v; want the first request, not a later or folded one", *cold)
	}
	if tally.usage.CacheHitTokens != 9400 {
		t.Fatalf("session totals = %d hit; the cold start must also count toward them", tally.usage.CacheHitTokens)
	}
}

// Sidecar calls carry their own prefixes, so folding one in would report a
// different prompt than the one the number is about.
func TestUsageTallyColdStartIgnoresNonExecutorSources(t *testing.T) {
	tally := newUsageTally()
	for _, source := range []string{
		event.UsageSourcePlanner, event.UsageSourceSubagent,
		event.UsageSourceCompaction, event.UsageSourceTitle,
	} {
		tally.observe(usageEvent(source, 500, 500, 0))
	}
	if tally.usage.ColdStart != nil {
		t.Fatalf("a sidecar call was recorded as the cold start: %+v", *tally.usage.ColdStart)
	}

	tally.observe(usageEvent("", 9000, 400, 8600))
	if tally.usage.ColdStart == nil || tally.usage.ColdStart.PromptTokens != 9000 {
		t.Fatal("an empty source must count as the executor, which is how every other reader resolves it")
	}
	if got := tally.usage.Sources[event.UsageSourcePlanner].RequestCount; got != 1 {
		t.Fatalf("planner requests = %d; the split is what says which component spent it", got)
	}
}

// A session that billed nothing writes nothing: a record of zeros cannot be
// told from a session that made no calls.
func TestUsageTallyWritesOnlyWhatItBilled(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")

	tally := newUsageTally()
	if err := tally.writeTo(path); err != nil {
		t.Fatalf("writeTo: %v", err)
	}
	if _, ok := usagereport.Load(path); ok {
		t.Fatal("a session that billed nothing left a record behind")
	}

	tally.observe(usageEvent(event.UsageSourceExecutor, 9000, 400, 8600))
	if err := tally.writeTo(path); err != nil {
		t.Fatalf("writeTo: %v", err)
	}
	report, ok := usagereport.Load(path)
	if !ok {
		t.Fatal("no record beside a session that billed a request")
	}
	if report.Version != usagereport.Version || report.Usage.ColdStart == nil {
		t.Fatalf("record = %+v; want the schema version and the cold start", report)
	}
	if report.Usage.ColdStart.CacheHitTokens != 400 {
		t.Fatalf("cold start hit = %d, want 400", report.Usage.ColdStart.CacheHitTokens)
	}
}

// The tee is the one layer every billable call passes through, whichever agent
// made it, so the wiring is what makes the record complete rather than partial.
func TestGoalUsageTeeFeedsTheSessionTally(t *testing.T) {
	tee, ok := NewGoalUsageTee(event.Discard).(*goalUsageTee)
	if !ok {
		t.Fatal("NewGoalUsageTee returned another type")
	}
	tee.Emit(usageEvent(event.UsageSourceExecutor, 9000, 400, 8600))

	if tee.tally == nil || tee.tally.usage.RequestCount != 1 {
		t.Fatal("a billable usage event did not reach the session tally")
	}
}
