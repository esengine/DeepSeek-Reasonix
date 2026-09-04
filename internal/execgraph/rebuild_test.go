package execgraph

import (
	"slices"
	"testing"
	"time"

	"reasonix/internal/agentgraph"
	"reasonix/internal/execjournal"
)

var base = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func at(seconds int) time.Time { return base.Add(time.Duration(seconds) * time.Second) }

func opened(id string) execjournal.Entry {
	return execjournal.Entry{ID: id, Group: "call", Kind: "worker", OpenedAt: at(0)}
}

func stateOf(g agentgraph.Graph, id string) agentgraph.NodeState {
	n, _ := g.Node(id)
	return n.State
}

func hasEdge(g agentgraph.Graph, from, to string, kind agentgraph.EdgeKind) bool {
	return slices.Contains(g.Edges, agentgraph.Edge{From: from, To: to, Kind: kind})
}

// TestStateComesFromTheRightOwner walks every shape an execution leaves behind.
// The order of authority is the point: an adoption settled itself at the
// opening, the store owns what happened to anything that ran, and only what
// neither of them answers is read off the lifecycle.
func TestStateComesFromTheRightOwner(t *testing.T) {
	for name, tc := range map[string]struct {
		entry    execjournal.Entry
		terminal string
		live     bool
		want     agentgraph.NodeState
		wantCut  bool
	}{
		"adopted, whatever else": {
			entry: execjournal.Entry{ID: "call/a", Disposition: execjournal.DispositionAdopted, AdoptedFrom: "sa_x"},
			want:  agentgraph.StateAdopted,
		},
		"completed in the store": {
			entry: settled(started(opened("call/a"), 2), 3), terminal: ChildCompleted,
			want: agentgraph.StateCompleted,
		},
		"failed in the store": {
			entry: settled(started(opened("call/a"), 2), 3), terminal: ChildFailed,
			want: agentgraph.StateFailed,
		},
		"cancelled in the store": {
			entry: settled(started(opened("call/a"), 2), 3), terminal: ChildCancelled,
			want: agentgraph.StateCancelled,
		},
		"running with an owner": {
			entry: started(opened("call/a"), 2), live: true,
			want: agentgraph.StateRunning,
		},
		"queued with an owner": {
			entry: queued(opened("call/a"), 1, "slots"), live: true,
			want: agentgraph.StateQueued,
		},
		"pending with an owner": {
			entry: opened("call/a"), live: true,
			want: agentgraph.StatePending,
		},
		"running with no owner": {
			entry: started(opened("call/a"), 2),
			want:  "", wantCut: true,
		},
		"queued with no owner": {
			entry: queued(opened("call/a"), 1, "slots"),
			want:  "", wantCut: true,
		},
		"settled having reached the scheduler": {
			entry: settled(queued(opened("call/a"), 1, "slots"), 3),
			want:  agentgraph.StateCancelled,
		},
		"settled without reaching it": {
			entry: settled(opened("call/a"), 3),
			want:  agentgraph.StateSkipped,
		},
	} {
		t.Run(name, func(t *testing.T) {
			children := []ChildOutcome{}
			if tc.terminal != "" {
				children = append(children, ChildOutcome{Execution: tc.entry.ID, Status: tc.terminal, Ref: "sa_ref"})
			}
			got := Rebuild([]execjournal.Entry{tc.entry}, children, ownerOf(tc.live))
			if state := stateOf(got.Graph, tc.entry.ID); state != tc.want {
				t.Errorf("state = %q, want %q", state, tc.want)
			}
			if cut := len(got.Interrupted) > 0; cut != tc.wantCut {
				t.Errorf("interrupted = %v, want %v", cut, tc.wantCut)
			}
		})
	}
}

// TestNothingIsPutBackAsLive is the invariant the whole rebuild exists under. A
// process that finds work open owns none of it, and a graph that showed any of
// it running or queued would be describing work nobody is doing.
func TestNothingIsPutBackAsLive(t *testing.T) {
	entries := []execjournal.Entry{
		started(opened("call/running"), 2),
		queued(opened("call/waiting"), 1, "writers"),
		opened("call/pending"),
	}
	got := Rebuild(entries, nil, nil)
	for _, n := range got.Graph.Nodes {
		switch n.State {
		case agentgraph.StateRunning, agentgraph.StateQueued, agentgraph.StatePending:
			t.Errorf("%s came back as %q with no owner", n.ID, n.State)
		}
	}
	if len(got.Interrupted) != 3 {
		t.Fatalf("interrupted = %d, want all three", len(got.Interrupted))
	}
	for _, i := range got.Interrupted {
		if want := i.Execution == "call/running"; i.Started != want {
			t.Errorf("%s started = %v, want %v", i.Execution, i.Started, want)
		}
	}
}

// TestWaitCauseSurvivesStarting: the cause is why admission was first refused,
// not what is holding the item now, so an item that went on to run keeps it.
func TestWaitCauseSurvivesStarting(t *testing.T) {
	entry := settled(started(queued(opened("call/a"), 1, "slots"), 2), 3)
	got := Rebuild([]execjournal.Entry{entry}, []ChildOutcome{{Execution: "call/a", Status: ChildCompleted}}, nil)
	n, _ := got.Graph.Node("call/a")
	if n.State != agentgraph.StateCompleted {
		t.Fatalf("state = %q, want completed", n.State)
	}
	if n.Wait != agentgraph.WaitSlots {
		t.Errorf("wait = %q, want the cause that queued it", n.Wait)
	}
}

// TestEdgesFollowFromTheOpenings: spawn and depends are stated, adopt names its
// source, and context is derived — an upstream that answered delivered to
// whatever started behind it, and one that did not delivered nothing.
func TestEdgesFollowFromTheOpenings(t *testing.T) {
	up := settled(started(opened("call/up"), 1), 2)
	dead := settled(started(opened("call/dead"), 1), 2)
	down := withDeps(started(opened("call/down"), 3), "call/up", "call/dead")
	adopting := execjournal.Entry{
		ID: "call/reuse", Group: "call", Kind: "worker",
		Disposition: execjournal.DispositionAdopted, AdoptedFrom: "sa_source",
	}
	children := []ChildOutcome{
		{Execution: "call/up", Status: ChildCompleted},
		{Execution: "call/dead", Status: ChildFailed},
	}

	got := Rebuild([]execjournal.Entry{up, dead, down, adopting}, children, ownerOf(true))
	g := got.Graph
	for _, want := range []struct {
		from, to string
		kind     agentgraph.EdgeKind
	}{
		{"call", "call/up", agentgraph.Spawn},
		{"call/up", "call/down", agentgraph.Depends},
		{"call/dead", "call/down", agentgraph.Depends},
		{"call/up", "call/down", agentgraph.Context},
		{"sa_source", "call/reuse", agentgraph.Adopt},
	} {
		if !hasEdge(g, want.from, want.to, want.kind) {
			t.Errorf("missing %s edge %s -> %s", want.kind, want.from, want.to)
		}
	}
	if hasEdge(g, "call/dead", "call/down", agentgraph.Context) {
		t.Error("an upstream that did not answer delivered nothing")
	}
	if n, _ := g.Node("sa_source"); n.Kind != agentgraph.KindExternal {
		t.Errorf("adoption source kind = %q, want external", n.Kind)
	}
}

// TestSkipCauseIsTheFirstUnansweredReleased: a fan-out cuts a branch when it
// processes the first result that did not complete and releases that item in
// the same handler, so the cause is the earliest released upstream that holds
// no answer — not the first one declared.
func TestSkipCauseIsTheFirstUnansweredReleased(t *testing.T) {
	children := []ChildOutcome{
		{Execution: "call/a", Status: ChildFailed},
		{Execution: "call/b", Status: ChildFailed},
	}
	for name, tc := range map[string]struct {
		aAt, bAt int
		want     string
	}{
		"a released first": {aAt: 2, bAt: 5, want: "call/a"},
		"b released first": {aAt: 5, bAt: 2, want: "call/b"},
	} {
		t.Run(name, func(t *testing.T) {
			entries := []execjournal.Entry{
				settled(started(opened("call/a"), 1), tc.aAt),
				settled(started(opened("call/b"), 1), tc.bAt),
				withDeps(settled(opened("call/c"), 6), "call/a", "call/b"),
			}
			if got := SkipCause(entries, children, "call/c"); got != tc.want {
				t.Errorf("cause = %q, want %q", got, tc.want)
			}
			g := Rebuild(entries, children, nil).Graph
			if state := stateOf(g, "call/c"); state != agentgraph.StateSkipped {
				t.Errorf("state = %q, want skipped", state)
			}
		})
	}
}

// TestRebuildIsDeterministic: two folds of the same facts must draw the same
// picture, or a comparison against one of them proves nothing about the other.
func TestRebuildIsDeterministic(t *testing.T) {
	entries := []execjournal.Entry{
		settled(started(opened("call/a"), 1), 2),
		withDeps(settled(opened("call/b"), 3), "call/a"),
		{ID: "call/c", Group: "call", Kind: "worker", Disposition: execjournal.DispositionAdopted, AdoptedFrom: "sa_x"},
	}
	children := []ChildOutcome{{Execution: "call/a", Status: ChildFailed}}
	first, second := Rebuild(entries, children, nil), Rebuild(entries, children, nil)
	if len(first.Graph.Nodes) != len(second.Graph.Nodes) {
		t.Fatalf("node counts differ: %d vs %d", len(first.Graph.Nodes), len(second.Graph.Nodes))
	}
	for i := range first.Graph.Nodes {
		if first.Graph.Nodes[i] != second.Graph.Nodes[i] {
			t.Fatalf("node %d differs:\n%+v\n%+v", i, first.Graph.Nodes[i], second.Graph.Nodes[i])
		}
	}
	if !slices.Equal(first.Graph.Edges, second.Graph.Edges) {
		t.Fatalf("edges differ:\n%+v\n%+v", first.Graph.Edges, second.Graph.Edges)
	}
}

func ownerOf(live bool) Owned {
	if !live {
		return nil
	}
	return func(string) bool { return true }
}

func queued(e execjournal.Entry, seconds int, cause string) execjournal.Entry {
	e.QueuedAt, e.Cause = at(seconds), cause
	return e
}

func started(e execjournal.Entry, seconds int) execjournal.Entry {
	e.StartedAt = at(seconds)
	return e
}

func settled(e execjournal.Entry, seconds int) execjournal.Entry {
	e.SettledAt = at(seconds)
	return e
}

func withDeps(e execjournal.Entry, deps ...string) execjournal.Entry {
	e.DependsOn = deps
	return e
}

// TestWorkerIdentityComesOnlyFromTheOpening: the store resolves inheritance and
// the opening does not, so an inherited blank must stay blank. Filling it from
// the store would make the rebuild more specific than the graph it rebuilds,
// deleting the fact that the worker layer named nothing.
func TestWorkerIdentityComesOnlyFromTheOpening(t *testing.T) {
	for name, tc := range map[string]struct {
		worker            *execjournal.WorkerSpec
		wantModel, wantEf string
	}{
		"named both":      {&execjournal.WorkerSpec{Model: "probe/alt", Effort: "high"}, "probe/alt", "high"},
		"named an effort": {&execjournal.WorkerSpec{Effort: "low"}, "", "low"},
		"named nothing":   {&execjournal.WorkerSpec{}, "", ""},
		"never recorded":  {nil, "", ""},
	} {
		t.Run(name, func(t *testing.T) {
			e := settled(started(opened("call/a"), 1), 2)
			e.Worker = tc.worker
			children := []ChildOutcome{{Execution: "call/a", Status: ChildCompleted}}
			g := Rebuild([]execjournal.Entry{e}, children, nil).Graph
			n, _ := g.Node("call/a")
			if n.Model != tc.wantModel || n.Effort != tc.wantEf {
				t.Fatalf("identity = %q/%q, want %q/%q", n.Model, n.Effort, tc.wantModel, tc.wantEf)
			}
		})
	}
}
