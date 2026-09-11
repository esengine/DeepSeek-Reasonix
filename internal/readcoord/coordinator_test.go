package readcoord

import (
	"testing"
	"time"

	"reasonix/internal/tool"
)

func envelope(readID, path, version string, intent tool.ReadIntent, requested *tool.ReadRange, delivered []tool.ReadRange, eof bool) tool.ReadResultEnvelope {
	env := tool.ReadResultEnvelope{
		ProtocolVersion: tool.ReadResultProtocolVersion,
		ReadID:          readID,
		Intent:          intent,
		Source:          tool.ReadResultSource{CanonicalPath: path, Snapshot: version},
		DeliveredRanges: delivered,
		EOF:             eof,
		HasMore:         !eof,
	}
	if len(delivered) > 0 {
		next := tool.ReadCursor{Path: path, ReadID: readID, Snapshot: version, NextStart: delivered[len(delivered)-1].End}
		env.NextCursor = tool.EncodeReadCursor(next)
	}
	if eof {
		end := 0
		for _, r := range delivered {
			end = max(end, r.End)
		}
		env.SourceEnd = &end
	}
	if requested != nil {
		env.RequestedRange = requested
	}
	return env
}

func TestInspectObligationEndsAfterOnePage(t *testing.T) {
	c := New()
	env := envelope("ir-1", "/w/a.go", "rw1:v1", tool.ReadIntentInspect, nil, ranges(0, 2000), false)
	tr, ok := c.Observe(env, 0)
	if !ok {
		t.Fatal("inspect delivery must be folded")
	}
	if tr.To != StateSatisfied || !tr.Progress || len(tr.Missing) != 0 {
		t.Fatalf("transition = %+v, want satisfied with no missing coverage", tr)
	}
	if _, ok := c.Observe(env, 0); ok {
		t.Fatal("a satisfied obligation must ignore a late delivery")
	}
}

func TestRangeObligationPagesUntilCovered(t *testing.T) {
	c := New()
	scope := Scope{WorkspaceID: "ws", CanonicalPath: "/w/a.go"}
	req := Requirement{Intent: tool.ReadIntentRange, Ranges: ranges(0, 20)}
	c.Begin("ir-1", scope, req)

	first := envelope("ir-1", "/w/a.go", "rw1:v1", tool.ReadIntentRange, &tool.ReadRange{Start: 0, End: 20}, ranges(0, 10), false)
	tr, ok := c.Observe(first, 0)
	if !ok || tr.To != StateNeedsMore {
		t.Fatalf("first page transition = %+v (ok=%v), want needs_more", tr, ok)
	}
	if !sameRanges(tr.Missing, ranges(10, 20)) {
		t.Fatalf("Missing = %+v, want 10-20", tr.Missing)
	}

	second := envelope("ir-1", "/w/a.go", "rw1:v1", tool.ReadIntentRange, &tool.ReadRange{Start: 0, End: 20}, ranges(10, 20), false)
	tr, ok = c.Observe(second, 0)
	if !ok || tr.To != StateSatisfied || len(tr.Missing) != 0 {
		t.Fatalf("second page transition = %+v (ok=%v), want satisfied", tr, ok)
	}
	ob, _ := c.Get("ir-1")
	if !sameRanges(ob.Covered, ranges(0, 20)) || ob.Pages != 2 {
		t.Fatalf("obligation = %+v, want covered 0-20 over 2 pages", ob)
	}
}

func TestRangeObligationIsSatisfiedByEOFShortOfWindow(t *testing.T) {
	c := New()
	req := Requirement{Intent: tool.ReadIntentRange, Ranges: ranges(0, 20)}
	c.Begin("ir-1", Scope{CanonicalPath: "/w/a.go"}, req)
	tr, ok := c.Observe(envelope("ir-1", "/w/a.go", "rw1:v1", tool.ReadIntentRange, &tool.ReadRange{Start: 0, End: 20}, ranges(0, 5), true), 0)
	if !ok || tr.To != StateSatisfied {
		t.Fatalf("EOF short of the window must satisfy the range: %+v (ok=%v)", tr, ok)
	}
}

func TestWholeFileRequiresContiguousCoverageFromLineZero(t *testing.T) {
	c := New()
	req := Requirement{Intent: tool.ReadIntentFull, WholeFile: true}
	c.Begin("ir-1", Scope{CanonicalPath: "/w/a.go"}, req)

	tr, _ := c.Observe(envelope("ir-1", "/w/a.go", "rw1:v1", tool.ReadIntentFull, nil, ranges(10, 20), true), 0)
	if tr.To != StateNeedsMore {
		t.Fatalf("tail-only delivery must not satisfy a whole-file read: %+v", tr)
	}
	if !sameRanges(tr.Missing, ranges(0, 10)) {
		t.Fatalf("Missing = %+v, want 0-10", tr.Missing)
	}
	tr, _ = c.Observe(envelope("ir-1", "/w/a.go", "rw1:v1", tool.ReadIntentFull, nil, ranges(0, 10), false), 0)
	if tr.To != StateSatisfied {
		t.Fatalf("contiguous coverage from line 0 after EOF must satisfy: %+v", tr)
	}
}

func TestWholeFileWithoutEOFStaysNeedsMore(t *testing.T) {
	c := New()
	c.Begin("ir-1", Scope{CanonicalPath: "/w/a.go"}, Requirement{Intent: tool.ReadIntentFull, WholeFile: true})
	tr, _ := c.Observe(envelope("ir-1", "/w/a.go", "rw1:v1", tool.ReadIntentFull, nil, ranges(0, 100), false), 0)
	if tr.To != StateNeedsMore {
		t.Fatalf("coverage without EOF cannot prove the whole file: %+v", tr)
	}
}

func TestVersionChangeResetsCoverageAndBumpsGeneration(t *testing.T) {
	c := New()
	c.Begin("ir-1", Scope{CanonicalPath: "/w/a.go"}, Requirement{Intent: tool.ReadIntentFull, WholeFile: true})
	c.Observe(envelope("ir-1", "/w/a.go", "rw1:v1", tool.ReadIntentFull, nil, ranges(0, 10), false), 0)

	tr, ok := c.Observe(envelope("ir-1", "/w/a.go", "rw1:v2", tool.ReadIntentFull, nil, ranges(10, 20), true), 0)
	if !ok || !tr.Stale || tr.Generation != 1 {
		t.Fatalf("version change must be reported stale with a new generation: %+v (ok=%v)", tr, ok)
	}
	ob, _ := c.Get("ir-1")
	if !sameRanges(ob.Covered, ranges(10, 20)) {
		t.Fatalf("Covered = %+v, want only the v2 delivery", ob.Covered)
	}
	if tr.To != StateNeedsMore || !sameRanges(tr.Missing, ranges(0, 10)) {
		t.Fatalf("after a version change the missing prefix must be reported: %+v", tr)
	}
}

func TestOutOfOrderPagesStillSatisfyAWholeFileRead(t *testing.T) {
	c := New()
	c.Begin("ir-1", Scope{CanonicalPath: "/w/a.go"}, Requirement{Intent: tool.ReadIntentFull, WholeFile: true})
	for _, r := range [][]tool.ReadRange{ranges(20, 30), ranges(0, 10), ranges(10, 20)} {
		eof := r[0].Start == 20
		c.Observe(envelope("ir-1", "/w/a.go", "rw1:v1", tool.ReadIntentFull, nil, r, eof), 0)
	}
	ob, _ := c.Get("ir-1")
	if ob.State != StateSatisfied {
		t.Fatalf("state = %s, want satisfied regardless of delivery order", ob.State)
	}
	if !sameRanges(ob.Covered, ranges(0, 30)) {
		t.Fatalf("Covered = %+v, want 0-30", ob.Covered)
	}
}

func TestRepeatedPageIsNotProgress(t *testing.T) {
	c := New()
	req := Requirement{Intent: tool.ReadIntentRange, Ranges: ranges(0, 40)}
	c.Begin("ir-1", Scope{CanonicalPath: "/w/a.go"}, req)
	page := envelope("ir-1", "/w/a.go", "rw1:v1", tool.ReadIntentRange, &tool.ReadRange{Start: 0, End: 40}, ranges(0, 10), false)
	if tr, _ := c.Observe(page, 0); !tr.Progress {
		t.Fatal("first delivery is progress")
	}
	tr, _ := c.Observe(page, 0)
	if tr.Progress || len(tr.Added) != 0 {
		t.Fatalf("a repeated page must not count as progress: %+v", tr)
	}
	ob, _ := c.Get("ir-1")
	if ob.Stagnant != 1 {
		t.Fatalf("Stagnant = %d, want 1", ob.Stagnant)
	}
}

func TestCancelledObligationIgnoresLateDelivery(t *testing.T) {
	c := New()
	c.Begin("ir-1", Scope{CanonicalPath: "/w/a.go"}, Requirement{Intent: tool.ReadIntentFull, WholeFile: true})
	tr, ok := c.Cancel("ir-1")
	if !ok || tr.To != StateCancelled {
		t.Fatalf("cancel transition = %+v (ok=%v)", tr, ok)
	}
	if _, ok := c.Observe(envelope("ir-1", "/w/a.go", "rw1:v1", tool.ReadIntentFull, nil, ranges(0, 10), true), 0); ok {
		t.Fatal("a cancelled obligation must not accept a late delivery")
	}
	ob, _ := c.Get("ir-1")
	if ob.State != StateCancelled {
		t.Fatalf("state = %s, want cancelled", ob.State)
	}
}

func TestStopReasonsAreReportedAndClearedByADelivery(t *testing.T) {
	c := New()
	c.Begin("ir-1", Scope{CanonicalPath: "/w/a.go"}, Requirement{Intent: tool.ReadIntentFull, WholeFile: true})
	tr, ok := c.Fail("ir-1", Block{Code: "read_error", Detail: "permission denied", Recovery: "fix permissions"})
	if !ok || tr.To != StateBlocked || tr.Stop == nil || tr.Stop.Code != "read_error" {
		t.Fatalf("fail transition = %+v (ok=%v)", tr, ok)
	}
	tr, ok = c.Narrow("ir-1", Block{Code: "budget", Detail: "context window unknown", Recovery: "read a narrower window"})
	if !ok || tr.To != StateNeedsScope || tr.Stop == nil || tr.Stop.Code != "budget" {
		t.Fatalf("narrow transition = %+v (ok=%v)", tr, ok)
	}
	ob, _ := c.Get("ir-1")
	if !ob.Requirement.WholeFile {
		t.Fatal("narrowing must not silently downgrade a whole-file requirement")
	}
	tr, ok = c.Observe(envelope("ir-1", "/w/a.go", "rw1:v1", tool.ReadIntentFull, nil, ranges(0, 10), true), 0)
	if !ok || tr.To != StateSatisfied || tr.Stop != nil {
		t.Fatalf("a delivery must clear the stop reason: %+v (ok=%v)", tr, ok)
	}
}

func TestBeginRefreshesRequirementAndKeepsCoverage(t *testing.T) {
	c := New()
	c.Begin("ir-1", Scope{CanonicalPath: "/w/a.go"}, Requirement{Intent: tool.ReadIntentRange, Ranges: ranges(0, 10)})
	c.Observe(envelope("ir-1", "/w/a.go", "rw1:v1", tool.ReadIntentRange, &tool.ReadRange{Start: 0, End: 10}, ranges(0, 10), false), 0)
	c.Begin("ir-1", Scope{CanonicalPath: "/w/a.go"}, Requirement{Intent: tool.ReadIntentRange, Ranges: ranges(0, 20)})
	ob, _ := c.Get("ir-1")
	if !sameRanges(ob.Covered, ranges(0, 10)) {
		t.Fatalf("Covered = %+v, want the earlier delivery kept", ob.Covered)
	}
	tr, _ := c.Observe(envelope("ir-1", "/w/a.go", "rw1:v1", tool.ReadIntentRange, &tool.ReadRange{Start: 10, End: 20}, ranges(10, 20), false), 0)
	if tr.To != StateSatisfied {
		t.Fatalf("refreshed requirement transition = %+v, want satisfied", tr)
	}
}

func TestObserveIgnoresEnvelopesWithoutIdentity(t *testing.T) {
	c := New()
	if _, ok := c.Observe(tool.ReadResultEnvelope{Intent: tool.ReadIntentInspect}, 0); ok {
		t.Fatal("an envelope without a read id or path must be ignored")
	}
}

func TestObserveWithoutBeginDerivesTheRequirementFromIntent(t *testing.T) {
	c := New()
	if _, ok := c.Observe(envelope("ir-1", "/w/a.go", "rw1:v1", tool.ReadIntentFull, nil, ranges(0, 10), true), 0); !ok {
		t.Fatal("an unregistered full read must still be folded")
	}
	ob, _ := c.Get("ir-1")
	if ob.Requirement.Intent != tool.ReadIntentFull || !ob.Requirement.WholeFile {
		t.Fatalf("requirement = %+v, want a whole-file full read", ob.Requirement)
	}
	if ob.State != StateSatisfied {
		t.Fatalf("state = %s, want satisfied after a complete full read", ob.State)
	}

	inspect, ok := c.Observe(envelope("ir-2", "/w/b.go", "rw1:v1", tool.ReadIntentInspect, nil, ranges(0, 200), false), 0)
	if !ok || inspect.To != StateSatisfied {
		t.Fatalf("inspect transition = %+v (ok=%v)", inspect, ok)
	}
	if got, _ := c.Get("ir-2"); got.Requirement.WholeFile || len(got.Requirement.Ranges) != 0 {
		t.Fatalf("inspect requirement = %+v, want no coverage debt", got.Requirement)
	}
}

func TestSnapshotIsOrderedByKey(t *testing.T) {
	c := New()
	for _, key := range []string{"ir-b", "ir-a"} {
		c.Begin(key, Scope{CanonicalPath: "/w/" + key}, Requirement{Intent: tool.ReadIntentInspect})
	}
	snap := c.Snapshot()
	if len(snap) != 2 || snap[0].Key != "ir-a" || snap[1].Key != "ir-b" {
		t.Fatalf("Snapshot = %+v, want key order", snap)
	}
}

func TestReturnedObligationsAreDeepCopies(t *testing.T) {
	c := New()
	c.Begin("ir-1", Scope{CanonicalPath: "/w/a.go"}, Requirement{Intent: tool.ReadIntentRange, Ranges: ranges(0, 20)})
	c.Observe(envelope("ir-1", "/w/a.go", "rw1:v1", tool.ReadIntentRange, &tool.ReadRange{Start: 0, End: 20}, ranges(0, 10), false), 0)

	got, _ := c.Get("ir-1")
	got.Covered[0] = tool.ReadRange{Start: 99, End: 100}
	got.Requirement.Ranges[0] = tool.ReadRange{Start: 99, End: 100}

	again, _ := c.Get("ir-1")
	if !sameRanges(again.Covered, ranges(0, 10)) || !sameRanges(again.Requirement.Ranges, ranges(0, 20)) {
		t.Fatalf("mutating a returned obligation changed coordinator state: %+v", again)
	}
	snap := c.Snapshot()
	snap[0].Covered[0] = tool.ReadRange{Start: 1, End: 2}
	third, _ := c.Get("ir-1")
	if !sameRanges(third.Covered, ranges(0, 10)) {
		t.Fatal("mutating a snapshot changed coordinator state")
	}
}

func TestRangeCompletionNeedsATrustworthySourceEnd(t *testing.T) {
	c := New()
	c.Begin("ir-1", Scope{CanonicalPath: "/w/a.go"}, Requirement{Intent: tool.ReadIntentRange, Ranges: ranges(0, 20)})

	// EOF without a source end proves nothing: the reader may have stopped early.
	unvouched := envelope("ir-1", "/w/a.go", "rw1:v1", tool.ReadIntentRange, &tool.ReadRange{Start: 0, End: 20}, ranges(0, 5), true)
	unvouched.SourceEnd = nil
	if tr, _ := c.Observe(unvouched, 0); tr.To != StateNeedsMore {
		t.Fatalf("bare EOF must not complete a range: %+v", tr)
	}

	// A source end inside the requested window does complete it.
	shortFile := envelope("ir-1", "/w/a.go", "rw1:v1", tool.ReadIntentRange, &tool.ReadRange{Start: 0, End: 20}, ranges(0, 5), true)
	shortFile.SourceEnd = new(int)
	*shortFile.SourceEnd = 5
	if tr, _ := c.Observe(shortFile, 0); tr.To != StateSatisfied {
		t.Fatalf("a source end inside the window completes the range: %+v", tr)
	}
}

func TestStalledPagesPivotOnceThenPause(t *testing.T) {
	c := NewWithPolicy(Policy{MaxPages: 64, PivotAfter: 2, PauseAfter: 2})
	c.Begin("ir-1", Scope{CanonicalPath: "/w/a.go"}, Requirement{Intent: tool.ReadIntentRange, Ranges: ranges(0, 40)})
	page := envelope("ir-1", "/w/a.go", "rw1:v1", tool.ReadIntentRange, &tool.ReadRange{Start: 0, End: 40}, ranges(0, 10), false)
	c.Observe(page, 0)

	if tr, _ := c.Observe(page, 0); tr.Advice != "" {
		t.Fatalf("one stalled page must not pivot yet: %+v", tr)
	}
	tr, _ := c.Observe(page, 0)
	if tr.Advice != AdvicePivot {
		t.Fatalf("the second stalled page must pivot once: %+v", tr)
	}
	c.Observe(page, 0)
	tr, _ = c.Observe(page, 0)
	if tr.To != StateBlocked || tr.Stop == nil || tr.Stop.Code != "no_progress" {
		t.Fatalf("two stalled pages after the pivot must pause the read: %+v", tr)
	}
}

func TestPageBudgetStopsContinuation(t *testing.T) {
	c := NewWithPolicy(Policy{MaxPages: 2})
	c.Begin("ir-1", Scope{CanonicalPath: "/w/a.go"}, Requirement{Intent: tool.ReadIntentRange, Ranges: ranges(0, 100)})
	for _, r := range [][]tool.ReadRange{ranges(0, 10), ranges(10, 20), ranges(20, 30)} {
		env := envelope("ir-1", "/w/a.go", "rw1:v1", tool.ReadIntentRange, &tool.ReadRange{Start: 0, End: 100}, r, false)
		tr, _ := c.Observe(env, 0)
		if r[0].Start == 20 {
			if tr.To != StateBlocked || tr.Stop == nil || tr.Stop.Code != "page_budget" {
				t.Fatalf("the page budget must stop continuation: %+v", tr)
			}
		}
	}
}

func TestActiveTimeBudgetStopsContinuation(t *testing.T) {
	c := NewWithPolicy(Policy{MaxActiveTime: 100 * time.Millisecond})
	c.Begin("ir-1", Scope{CanonicalPath: "/w/a.go"}, Requirement{Intent: tool.ReadIntentRange, Ranges: ranges(0, 100)})
	env := envelope("ir-1", "/w/a.go", "rw1:v1", tool.ReadIntentRange, &tool.ReadRange{Start: 0, End: 100}, ranges(0, 10), false)
	tr, _ := c.Observe(env, 200)
	if tr.To != StateBlocked || tr.Stop == nil || tr.Stop.Code != "time_budget" {
		t.Fatalf("the active-time budget must stop continuation: %+v", tr)
	}
}

func TestContentChangeDoesNotResetTheBudget(t *testing.T) {
	c := NewWithPolicy(Policy{MaxPages: 1})
	c.Begin("ir-1", Scope{CanonicalPath: "/w/a.go"}, Requirement{Intent: tool.ReadIntentFull, WholeFile: true})
	c.Observe(envelope("ir-1", "/w/a.go", "rw1:v1", tool.ReadIntentFull, nil, ranges(0, 10), false), 0)

	tr, _ := c.Observe(envelope("ir-1", "/w/a.go", "rw1:v2", tool.ReadIntentFull, nil, ranges(10, 20), false), 0)
	if tr.To != StateBlocked || tr.Stop == nil || tr.Stop.Code != "page_budget" {
		t.Fatalf("a content change must not reset the hard budget: %+v", tr)
	}
}
