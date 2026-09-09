package readcoord

import (
	"testing"
	"time"

	"reasonix/internal/tool"
)

func TestAssociateExpandsOnlyMissingCoverageWithoutResettingBudget(t *testing.T) {
	c := New()
	end := 4
	env := tool.ReadResultEnvelope{ReadID: "first", Source: tool.ReadResultSource{CanonicalPath: "/w/a", Identity: "raw", Snapshot: "v"}, Intent: tool.ReadIntentRange, RequestedRange: &tool.ReadRange{Start: 0, End: 2}, DeliveredRanges: []tool.ReadRange{{Start: 0, End: 2}}, SourceEnd: &end}
	c.Observe(env, 5)
	env.ReadID, env.Intent, env.RequestedRange = "new-call", tool.ReadIntentFull, nil
	env.ReadID = c.Associate(env)
	if env.ReadID != "first" {
		t.Fatal("lost previous coverage")
	}
	tr, _ := c.Observe(env, 7)
	if tr.To != StateNeedsMore || !Covers(tr.Missing, []tool.ReadRange{{Start: 2, End: 4}}) {
		t.Fatalf("missing ranges: %+v", tr)
	}
	ob, _ := c.Get("first")
	if ob.Pages != 2 || ob.ActiveTime != 12*time.Millisecond {
		t.Fatal("expanded requirement reset accounting")
	}
	env.DeliveredRanges, env.EOF = []tool.ReadRange{{Start: 2, End: 4}}, true
	tr, _ = c.Observe(env, 3)
	if tr.To != StateSatisfied {
		t.Fatal("tail failed to finish expanded requirement")
	}
}
