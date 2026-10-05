package tool

import "testing"

type reachingTool struct {
	stubTool
	reach Reach
}

func (r reachingTool) Reach() Reach { return r.reach }

func admitReads(t Tool) bool { return ReachOf(t) == ReachLocalRead }

func TestRestrictDropsWhatIsHeldAndRefusesWhatComesLater(t *testing.T) {
	r := NewRegistry()
	r.Add(reachingTool{stubTool{name: "read"}, ReachLocalRead})
	r.Add(stubTool{name: "unclassified"})
	r.Add(reachingTool{stubTool{name: "control"}, ReachHostControl})
	before := r.SchemaRevision()

	r.Restrict(admitReads)
	if got := r.AllNames(); len(got) != 1 || got[0] != "read" {
		t.Fatalf("after Restrict the registry holds %v, want only read", got)
	}
	if r.SchemaRevision() == before {
		t.Fatal("dropping tools left the schema revision unchanged")
	}

	r.Add(stubTool{name: "late"})
	r.Add(reachingTool{stubTool{name: "read2"}, ReachLocalRead})
	r.Add(reachingTool{stubTool{name: "read"}, ReachHostControl})
	if got := r.AllNames(); len(got) != 2 || got[0] != "read" || got[1] != "read2" {
		t.Fatalf("registry holds %v, want read and read2 in order", got)
	}
	if tl, ok := r.Get("read"); !ok || ReachOf(tl) != ReachLocalRead {
		t.Fatal("a rejected replacement displaced an admitted tool")
	}
	if _, ok := r.Get("unclassified"); ok {
		t.Fatal("a tool the ceiling refuses is still resolvable")
	}
}

func TestReachOfIsUnstatedUntilDeclared(t *testing.T) {
	if got := ReachOf(stubTool{name: "x"}); got != ReachUnstated {
		t.Fatalf("ReachOf = %v, want the zero value", got)
	}
}
