package agent

import (
	"context"
	"errors"
	"fmt"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// A digest that carried everything costs no extra tokens: the backstop exists
// for what was dropped, not as a second copy of what was kept.
func TestBackstopIsSilentWhenTheDigestCarriedEverything(t *testing.T) {
	cov := measureFoldCoverage(coverageRegion(), coverageTools,
		"changed internal/parser/lexer.go and internal/parser/reader.go; go test failed")
	if got := foldCoverageBackstop(cov); got != "" {
		t.Fatalf("backstop = %q, want nothing added to a complete digest", got)
	}
}

// What the digest dropped is what the host writes, and only that.
func TestBackstopNamesOnlyTheDroppedFacts(t *testing.T) {
	cov := measureFoldCoverage(coverageRegion(), coverageTools,
		"reworked internal/parser/lexer.go along the way")
	block := foldCoverageBackstop(cov)
	if !strings.Contains(block, "internal/parser/reader.go") {
		t.Errorf("backstop = %q, want the change the digest dropped", block)
	}
	if !strings.Contains(block, "go test") {
		t.Errorf("backstop = %q, want the failure the digest dropped", block)
	}
	if strings.Contains(block, "lexer.go") {
		t.Errorf("backstop = %q, want it silent about what the digest already carried", block)
	}
	// Reads are not the digest's obligation and must not become the host's:
	// the fold index already carries them.
	if strings.Contains(block, "notes.md") {
		t.Errorf("backstop = %q, want reads left to the fold index", block)
	}
}

// A fold with more changes than the block holds says how many it left out. A
// list that just stops reads as a complete list.
func TestBackstopReportsWhatItTruncated(t *testing.T) {
	var region []provider.Message
	var calls []provider.ToolCall
	for i := range maxBackstopFacts + 5 {
		id := fmt.Sprintf("w%d", i)
		calls = append(calls, provider.ToolCall{
			ID: id, Name: "write_file", Arguments: fmt.Sprintf(`{"path":"internal/gen/f%02d.go"}`, i),
		})
	}
	region = append(region, provider.Message{Role: provider.RoleAssistant, ToolCalls: calls})
	for _, c := range calls {
		region = append(region, provider.Message{Role: provider.RoleTool, ToolCallID: c.ID, Name: "write_file", Content: "wrote"})
	}
	block := foldCoverageBackstop(measureFoldCoverage(region, coverageTools, "did some work"))
	if strings.Count(block, "- changed:") != maxBackstopFacts {
		t.Errorf("listed %d facts, want %d", strings.Count(block, "- changed:"), maxBackstopFacts)
	}
	if !strings.Contains(block, "5 more changed") {
		t.Errorf("backstop = %q, want it to say how many it left out", block)
	}
}

// foldableSessionWithChanges is bulk the free prune cannot reclaim, with two
// real changes and a failed command inside it, so a fold has facts to lose.
func foldableSessionWithChanges(turns int) *sessionstore.Session {
	big := strings.Repeat("word ", 400)
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "rework the parser"},
	}
	msgs = append(msgs, coverageRegion()...)
	for range turns {
		msgs = append(msgs,
			provider.Message{Role: provider.RoleAssistant, Content: big},
			provider.Message{Role: provider.RoleUser, Content: "continue"},
		)
	}
	return &sessionstore.Session{Messages: msgs}
}

func projectionDigest(a *Agent) string {
	return latestDigest(a.sess.win.compactionState.Projection.Messages)
}

// The hard-pressure path is the one that used to lose changes: the repair pass
// is skipped under mustFree, and the fold index skips a change on the digest's
// promise to carry it. Neither may end with the change gone from the model's
// world while the transcript still holds it.
func TestHardPressureDigestKeepsChangesItDroppedItself(t *testing.T) {
	sess := foldableSessionWithChanges(6)
	// A summary that says nothing about the changes, which is exactly the
	// digest repairFoldCoverage would have retried below the ceiling.
	a := agentOverForce(t, &fakeProvider{reply: "The team kept working on the project."}, sess)

	if err := prepareContext(context.Background(), a, CompactionTriggerOverflow); err != nil {
		t.Fatalf("prepare = %v", err)
	}
	digest := projectionDigest(a)
	for _, path := range []string{"internal/parser/lexer.go", "internal/parser/reader.go"} {
		if !strings.Contains(digest, path) {
			t.Errorf("digest lost %s entirely:\n%s", path, digest)
		}
	}
	if !strings.Contains(digest, foldBackstopHeading) {
		t.Errorf("no host backstop in the projection:\n%s", digest)
	}
}

// A summarizer that failed leaves a mechanical digest carrying nothing. The
// facts the host can prove must survive that too.
func TestDegradedFoldKeepsHostKnownFacts(t *testing.T) {
	sess := foldableSessionWithChanges(6)
	a := agentOverForce(t, &fakeProvider{streamErr: errors.New("provider down")}, sess)

	if err := prepareContext(context.Background(), a, CompactionTriggerOverflow); err != nil {
		t.Fatalf("prepare = %v, want a degraded fold", err)
	}
	digest := projectionDigest(a)
	if !strings.Contains(digest, "summary was unavailable") {
		t.Fatalf("fixture did not take the degraded path:\n%s", digest)
	}
	if !strings.Contains(digest, "internal/parser/reader.go") {
		t.Errorf("a degraded fold lost a change the host could prove:\n%s", digest)
	}
	if !strings.Contains(digest, "go test") {
		t.Errorf("a degraded fold lost a failure the host could prove:\n%s", digest)
	}
}

// A degraded fold used to report the same coverage as a perfect one, because
// nothing measured it. A meter that reads zero loss on total loss is worse than
// no meter.
func TestDegradedFoldReportsWhatItLost(t *testing.T) {
	sess := foldableSessionWithChanges(6)
	var compaction event.Compaction
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.CompactionDone {
			compaction = e.Compaction
		}
	})
	a := New(&fakeProvider{streamErr: errors.New("provider down")}, tool.NewRegistry(), sess, Options{
		ContextWindow: 5000, CompactRatio: 0.5, RecentKeep: 2, ArchiveDir: testenv.TempDir(t),
	}, sink)

	if err := prepareContext(context.Background(), a, CompactionTriggerOverflow); err != nil {
		t.Fatalf("prepare = %v", err)
	}
	if compaction.CoverageRequired == 0 {
		t.Fatalf("a fold with changes reported nothing to cover: %+v", compaction)
	}
	if compaction.CoverageMissing != compaction.CoverageRequired {
		t.Errorf("a mechanical digest reported %d/%d covered; it carried none of them",
			compaction.CoverageRequired-compaction.CoverageMissing, compaction.CoverageRequired)
	}
	if !compaction.CoverageBackstopped {
		t.Error("the host completed the digest but the receipt does not say so")
	}
}
