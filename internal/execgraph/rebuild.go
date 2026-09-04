package execgraph

import (
	"time"

	"reasonix/internal/agentgraph"
	"reasonix/internal/execjournal"
)

// Child terminals, as the sub-agent store spells them. They are strings rather
// than a shared type because the store is not in this package's world: a caller
// maps its own records onto ChildOutcome and nothing here depends on where they
// came from.
const (
	ChildCompleted = "completed"
	ChildFailed    = "failed"
	ChildCancelled = "cancelled"
)

// ChildOutcome is what the store kept about one child that actually ran, joined
// to the journal by the execution it ran for. The worker identity the store
// resolved is left out on purpose: the graph's Model carries the override a
// caller asked for, and filling it from the resolved one puts a third meaning
// on the field.
type ChildOutcome struct {
	Execution string
	Ref       string
	Status    string
}

// Interruption is an execution whose owner disappeared. Started separates the
// two kinds: one had reached a slot and may have done part of its work, the
// other had not begun and did nothing at all.
type Interruption struct {
	Execution string
	Started   bool
}

// Result is the graph the durable facts justify, and the executions no live
// state could honestly be assigned to.
type Result struct {
	Graph       agentgraph.Graph
	Interrupted []Interruption
}

// Owned reports whether this process still owns an execution. A caller with no
// live executions — the ordinary case after a restart — passes nil.
type Owned func(execution string) bool

// Rebuild folds the durable facts into a graph. Order is deterministic: nodes
// follow the order the executions were opened in, and edges follow their nodes,
// so two callers given the same inputs draw the same picture.
func Rebuild(entries []execjournal.Entry, children []ChildOutcome, owned Owned) Result {
	if owned == nil {
		owned = func(string) bool { return false }
	}
	byExecution := map[string]ChildOutcome{}
	for _, c := range children {
		if c.Execution != "" {
			byExecution[c.Execution] = c
		}
	}
	answered := answeredSet(entries, byExecution)

	var out Result
	for _, e := range entries {
		child := byExecution[e.ID]
		state, cut := resolveState(e, child.Status, owned(e.ID))
		if cut {
			out.Interrupted = append(out.Interrupted, Interruption{Execution: e.ID, Started: e.Started()})
		}
		out.Graph.Apply(agentgraph.Delta{Nodes: []agentgraph.Node{nodeFor(e, child, state)}})
		out.Graph.Apply(edgesFor(e, answered))
	}
	return out
}

// resolveState decides what a node stands at, and whether the execution behind
// it was cut. The order is the order of authority: an adoption declared itself
// at the opening, the store owns what happened to anything that ran, and only
// what neither of them settles is read off the lifecycle.
func resolveState(e execjournal.Entry, terminal string, live bool) (agentgraph.NodeState, bool) {
	if e.Disposition == execjournal.DispositionAdopted {
		return agentgraph.StateAdopted, false
	}
	if state, ok := terminalState(terminal); ok {
		return state, false
	}
	if e.SettledAt.IsZero() {
		if !live {
			// Running or waiting with no owner. Neither state may be put back:
			// both describe work someone is doing, and nobody is.
			return "", true
		}
		switch {
		case e.Started():
			return agentgraph.StateRunning, false
		case e.Queued():
			return agentgraph.StateQueued, false
		default:
			return agentgraph.StatePending, false
		}
	}
	// Settled with nothing in the store: it never ran. Reaching the scheduler
	// and being released without admission is a cancellation; never reaching it
	// is a branch the orchestration cut.
	if e.Queued() {
		return agentgraph.StateCancelled, false
	}
	return agentgraph.StateSkipped, false
}

func terminalState(status string) (agentgraph.NodeState, bool) {
	switch status {
	case ChildCompleted:
		return agentgraph.StateCompleted, true
	case ChildFailed:
		return agentgraph.StateFailed, true
	case ChildCancelled:
		return agentgraph.StateCancelled, true
	}
	return "", false
}

// answeredSet is which executions hold an answer a dependent may start from.
// An adoption says so on its own opening; everything else is the store's.
func answeredSet(entries []execjournal.Entry, children map[string]ChildOutcome) map[string]bool {
	out := map[string]bool{}
	for _, e := range entries {
		out[e.ID] = e.Disposition == execjournal.DispositionAdopted ||
			children[e.ID].Status == ChildCompleted
	}
	return out
}

func nodeFor(e execjournal.Entry, child ChildOutcome, state agentgraph.NodeState) agentgraph.Node {
	node := agentgraph.Node{
		ID: e.ID, ParentID: e.Group, Kind: agentgraph.NodeKind(e.Kind), State: state,
		Label: e.Name, Grant: agentgraph.Grant(e.Grant), Wait: agentgraph.WaitCause(e.Cause),
		Ref:      child.Ref,
		QueuedAt: milli(e.QueuedAt), StartedAt: milli(e.StartedAt), EndedAt: milli(e.SettledAt),
	}
	// Only from the opening. The store's resolved identity is a later layer:
	// filling an inherited blank from it would delete the fact that the worker
	// layer named nothing, and an entry that recorded none says nothing at all.
	if e.Worker != nil {
		node.Model, node.Effort = e.Worker.Model, e.Worker.Effort
	}
	return node
}

// edgesFor draws everything the opening implies. Spawn and depends are stated
// outright; adopt names the source the reuse came from; context is derived,
// because an upstream that answered delivered its answer to whatever started
// behind it.
func edgesFor(e execjournal.Entry, answered map[string]bool) agentgraph.Delta {
	var delta agentgraph.Delta
	if e.Group != "" {
		delta.Nodes = append(delta.Nodes, agentgraph.Node{ID: e.Group, Kind: agentgraph.KindGroup})
		delta.Edges = append(delta.Edges, agentgraph.Edge{From: e.Group, To: e.ID, Kind: agentgraph.Spawn})
	}
	if e.AdoptedFrom != "" {
		delta.Nodes = append(delta.Nodes, agentgraph.Node{
			ID: e.AdoptedFrom, Kind: agentgraph.KindExternal, State: agentgraph.StateCompleted,
		})
		delta.Edges = append(delta.Edges, agentgraph.Edge{From: e.AdoptedFrom, To: e.ID, Kind: agentgraph.Adopt})
	}
	for _, up := range e.DependsOn {
		delta.Edges = append(delta.Edges, agentgraph.Edge{From: up, To: e.ID, Kind: agentgraph.Depends})
		if e.Started() && answered[up] {
			delta.Edges = append(delta.Edges, agentgraph.Edge{From: up, To: e.ID, Kind: agentgraph.Context})
		}
	}
	return delta
}

// SkipCause names the upstream a cut branch was cut by: the first of its
// unanswered dependencies to be released. A fan-out cuts a branch when it
// processes the first result that did not complete, and releases that item in
// the same handler, so the order the journal records is the order the choice
// was made in.
func SkipCause(entries []execjournal.Entry, children []ChildOutcome, execution string) string {
	byExecution := map[string]ChildOutcome{}
	for _, c := range children {
		byExecution[c.Execution] = c
	}
	answered := answeredSet(entries, byExecution)
	index := map[string]execjournal.Entry{}
	for _, e := range entries {
		index[e.ID] = e
	}
	target, ok := index[execution]
	if !ok {
		return ""
	}
	cause := ""
	for _, up := range target.DependsOn {
		if answered[up] {
			continue
		}
		if cause == "" || index[up].SettledAt.Before(index[cause].SettledAt) {
			cause = up
		}
	}
	return cause
}

// milli renders a timestamp the way the graph carries them, keeping zero as
// zero: the graph reads an absent stamp as "not learned yet", and 1970 would
// read as a moment that happened.
func milli(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}
