package boot

// Effect test for the run graph: what a frontend can tell about a fan-out's
// shape once the whole real stack has run. The graph a fleet proves at
// preflight used to live and die inside one Execute call.

import (
	"maps"
	"slices"
	"sync"
	"testing"

	"reasonix/internal/contract/agentgraph"
	"reasonix/internal/contract/event"
)

// graphEffectCall dispatches a two-item fleet with a dependency in it. Every
// dispatcher hides behind use_capability, so this is also the shape the model
// really reaches fleet through.
const graphEffectCall = `{"action":"call","capability_id":"task:fleet","arguments":{"tasks":[` +
	`{"id":"research","description":"survey","prompt":"reply RESEARCH and stop","read_only":true},` +
	`{"id":"implement","description":"rewrite","prompt":"reply IMPL and stop","depends_on":["research"],"read_only":true}` +
	`]}}`

// graphEffectSink folds the published deltas exactly as a frontend does, and
// keeps the tool ids alongside them: a graph whose nodes cannot be matched to
// the transcript is a second naming scheme, which is the problem this replaced.
type graphEffectSink struct {
	mu      sync.Mutex
	graph   agentgraph.Graph
	deltas  int
	toolIDs map[string]bool
}

func newGraphEffectSink() *graphEffectSink {
	return &graphEffectSink{toolIDs: map[string]bool{}}
}

func (s *graphEffectSink) Emit(e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch e.Kind {
	case event.GraphDelta:
		if e.Graph == nil {
			return
		}
		s.deltas++
		s.graph.Apply(*e.Graph)
	case event.ToolDispatch:
		if id := e.Tool.ID; id != "" {
			s.toolIDs[id] = true
		}
	}
}

func (s *graphEffectSink) result() (agentgraph.Graph, int, map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.graph, s.deltas, s.toolIDs
}

// workersUnder returns the ids a group spawned, so the assertions read the
// shape rather than a naming convention they would then be pinning in place.
func workersUnder(g agentgraph.Graph, group string) []string {
	var out []string
	for _, e := range g.Edges {
		if e.Kind == agentgraph.Spawn && e.From == group {
			out = append(out, e.To)
		}
	}
	return out
}

func groupIn(t *testing.T, g agentgraph.Graph) string {
	t.Helper()
	for _, n := range g.Nodes {
		if n.Kind == agentgraph.KindGroup {
			return n.ID
		}
	}
	t.Fatalf("no group node reached the sink; nodes %+v", g.Nodes)
	return ""
}

// TestEffectRunGraphReachesTheFrontendWithItsEdges pins the promise at its
// final boundary: after a real turn, a frontend holds a graph that says which
// item waited for which, and whose nodes address the same calls the transcript
// already carries.
func TestEffectRunGraphReachesTheFrontendWithItsEdges(t *testing.T) {
	sink := newGraphEffectSink()
	runProbeWith(t, "boot-agent-graph", &capabilityProbeProvider{calls: []string{graphEffectCall}}, sink)

	g, deltas, toolIDs := sink.result()
	if deltas == 0 {
		t.Fatal("no graph delta reached the frontend sink: the run's shape never left the kernel")
	}

	group := groupIn(t, g)
	workers := workersUnder(g, group)
	if len(workers) != 2 {
		t.Fatalf("group %q spawned %v, want the two fleet items", group, workers)
	}

	var ordered []agentgraph.Edge
	for _, e := range g.Edges {
		if e.Kind == agentgraph.Depends {
			ordered = append(ordered, e)
		}
	}
	if len(ordered) != 1 {
		t.Fatalf("depends edges = %v, want exactly the one depends_on declared", ordered)
	}
	dep := ordered[0]
	// Ordering and delivery are separate facts, and both are true here: the
	// dependent started from the answer, it did not merely start after it.
	carried := agentgraph.Edge{From: dep.From, To: dep.To, Kind: agentgraph.Context}
	if !slices.Contains(g.Edges, carried) {
		t.Fatalf("the dependent was ordered but its upstream answer was never recorded as delivered: %v", g.Edges)
	}

	for _, id := range append([]string{group}, workers...) {
		node, ok := g.Node(id)
		if !ok {
			t.Fatalf("node %q vanished from the folded graph", id)
		}
		if node.State != agentgraph.StateCompleted {
			t.Fatalf("node %q finished %q, want completed; a clean run must not read as unfinished", id, node.State)
		}
		// Every figure that prices a fan-out is a subtraction of these two. A
		// node that settles without them reports the shape and none of its cost,
		// and a metric folded from it reads as a fan-out that took no time.
		if node.StartedAt == 0 || node.EndedAt < node.StartedAt {
			t.Fatalf("node %q settled unstamped (started %d, ended %d); nothing downstream can time it", id, node.StartedAt, node.EndedAt)
		}
	}
	for _, id := range workers {
		if node, _ := g.Node(id); node.QueuedAt == 0 {
			t.Fatalf("worker %q never reported reaching the queue; the wait a slot cost it is unrecoverable", id)
		}
	}
	// The unification: a node is the call it names. A frontend joins timing and
	// output to the graph by id instead of matching two naming schemes.
	for _, id := range workers {
		if !toolIDs[id] {
			t.Fatalf("graph node %q matches no dispatched tool call; dispatched %v", id, slices.Sorted(maps.Keys(toolIDs)))
		}
	}
}
