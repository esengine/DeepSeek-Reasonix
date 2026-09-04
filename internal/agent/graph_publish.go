package agent

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"reasonix/internal/agentgraph"
	"reasonix/internal/event"
)

// graphErrBudget bounds the failure text a node carries. A graph is drawn, not
// read line by line; the full error is already on the item's tool result.
const graphErrBudget = 256

// fanOutNodeID is the identity the rest of the stream already uses for one item
// of a fan-out. A caller-visible id stays a label: the model chooses it and it
// may be anything, while this one is namespaced and already carries the child's
// tool events, so a frontend joins timing and output to a node by id instead of
// matching two naming schemes.
func fanOutNodeID(parentID, prefix string, idx int) string {
	return fmt.Sprintf("%s/%s-%d", parentID, prefix, idx+1)
}

func fleetNodeID(parentID string, idx int) string { return fanOutNodeID(parentID, "fleet", idx) }

func parallelNodeID(parentID string, idx int) string { return fanOutNodeID(parentID, "sub", idx) }

// publishGraph emits one graph delta, or nothing when there is nothing to say.
func publishGraph(sink event.Sink, delta agentgraph.Delta) {
	if sink == nil || delta.Empty() {
		return
	}
	sink.Emit(event.Event{Kind: event.GraphDelta, Graph: &delta})
}

// fanOutOpeningDelta declares a group and the workers hanging from it before any
// of them runs. Publishing the shape up front is the point: a running fan-out
// becomes a picture of what each item is still waiting for, and an item that
// never runs at all emits no tool events to infer one from.
func fanOutOpeningDelta(parentID, label string, workers []agentgraph.Node) agentgraph.Delta {
	delta := agentgraph.Delta{Nodes: make([]agentgraph.Node, 0, len(workers)+1)}
	delta.Nodes = append(delta.Nodes, agentgraph.Node{
		ID: parentID, Kind: agentgraph.KindGroup, State: agentgraph.StateRunning, Label: label,
		StartedAt: nowMilli(),
	})
	for _, worker := range workers {
		delta.Nodes = append(delta.Nodes, worker)
		delta.Edges = append(delta.Edges, agentgraph.Edge{From: parentID, To: worker.ID, Kind: agentgraph.Spawn})
	}
	return delta
}

// fanOutOutcomeDelta reports where a group and its items ended. It carries
// outcomes only: identity was declared when the group opened, and the fold
// keeps what an update says nothing about.
func fanOutOutcomeDelta(parentID string, state agentgraph.NodeState, items []agentgraph.Node) agentgraph.Delta {
	nodes := make([]agentgraph.Node, 0, len(items)+1)
	nodes = append(nodes, agentgraph.Node{ID: parentID, State: state, EndedAt: nowMilli()})
	return agentgraph.Delta{Nodes: append(nodes, items...)}
}

func nowMilli() int64 { return time.Now().UnixMilli() }

// fanOutItemQueuedDelta reports an item handed to the session scheduler. Its
// dependencies are answered and it is still not running, so the only thing left
// between it and its first step is a free slot — the one wait that is invisible
// on the graph, because unlike a dependency it has no edge to draw it as.
func fanOutItemQueuedDelta(id string) agentgraph.Delta {
	return agentgraph.Delta{Nodes: []agentgraph.Node{
		{ID: id, State: agentgraph.StateQueued, QueuedAt: nowMilli()},
	}}
}

// fanOutItemWaitDelta names what is holding a queued item out of a slot. The
// state already says queued; which constraint said so is what decides whether
// raising a session ceiling would buy anything, and only the scheduler sees it.
func fanOutItemWaitDelta(id string, cause agentgraph.WaitCause) agentgraph.Delta {
	if cause == "" {
		return agentgraph.Delta{}
	}
	return agentgraph.Delta{Nodes: []agentgraph.Node{{ID: id, Wait: cause}}}
}

// fanOutItemHooks are the two moments of an item's wait that only the scheduler
// can report: what held it out of a slot, and the grant that ended that. The
// second never refuses; a caller that has something to persist first wraps it.
func fanOutItemHooks(sink event.Sink, id string) (func(agentgraph.WaitCause) error, func() error) {
	return func(cause agentgraph.WaitCause) error {
			publishGraph(sink, fanOutItemWaitDelta(id, cause))
			return nil
		},
		func() error { publishGraph(sink, fanOutItemRunningDelta(id)); return nil }
}

// fanOutItemRunningDelta reports the slot being granted. The gap back to
// QueuedAt is what the concurrency ceiling cost this item.
func fanOutItemRunningDelta(id string) agentgraph.Delta {
	return agentgraph.Delta{Nodes: []agentgraph.Node{
		{ID: id, State: agentgraph.StateRunning, StartedAt: nowMilli()},
	}}
}

// fanOutItemSettledDelta reports one item settling, at the moment it settles.
// The group publishes every item's outcome again when it ends, but waiting for
// that leaves a finished item drawn as still running for as long as its slowest
// sibling takes — on a long fan-out, the whole picture stays wrong until it is
// no longer worth looking at.
func fanOutItemSettledDelta(id string, state agentgraph.NodeState, ref string, err error) agentgraph.Delta {
	node := agentgraph.Node{ID: id, State: state, Ref: ref, EndedAt: nowMilli()}
	if err != nil {
		node.Err = boundedInline(err.Error(), graphErrBudget)
	}
	return agentgraph.Delta{Nodes: []agentgraph.Node{node}}
}

// groupTerminalState classifies a fan-out's single terminal outcome:
// cancellation/deadline wins, then any failed child, then any error (including
// validation failures), then completed.
func groupTerminalState(ctx context.Context, err error, states []agentgraph.NodeState) agentgraph.NodeState {
	switch {
	case ctx.Err() != nil:
		return agentgraph.StateCancelled
	case slices.Contains(states, agentgraph.StateFailed), err != nil:
		return agentgraph.StateFailed
	default:
		return agentgraph.StateCompleted
	}
}

// terminalProgressPhase renders a settled state as the progress card's phase,
// so a group's card and its graph node cannot disagree about how it ended.
func terminalProgressPhase(state agentgraph.NodeState) subagentProgressPhase {
	switch state {
	case agentgraph.StateCancelled:
		return subagentPhaseCancelled
	case agentgraph.StateCompleted, agentgraph.StateAdopted:
		return subagentPhaseCompleted
	default:
		return subagentPhaseFailed
	}
}

// fleetNodeLabel is what a person reads on the node: the item's description,
// falling back to the id it was addressed by when nothing ran to have one — an
// adopted item builds no spec.
func fleetNodeLabel(plan fleetPlan, specs []ProfileExecSpec, i int) string {
	if label := strings.TrimSpace(specs[i].Task.Description); label != "" {
		return label
	}
	return plan.ids[i]
}

// fleetOpeningDelta adds the edges only a fleet has to the shape every fan-out
// declares: what each item waits for, and whose finished answer stood in for
// running it.
func fleetOpeningDelta(parentID string, plan fleetPlan, specs []ProfileExecSpec, adopted map[int]adoptedItem, results []fleetItemResult) agentgraph.Delta {
	workers := make([]agentgraph.Node, 0, len(plan.ids))
	var extra agentgraph.Delta
	for i := range plan.ids {
		id := fleetNodeID(parentID, i)
		node := agentgraph.Node{
			ID: id, ParentID: parentID, Kind: agentgraph.KindWorker,
			State: results[i].status, Label: fleetNodeLabel(plan, specs, i),
		}
		if a, ok := adopted[i]; ok {
			// The answer's source is a child some earlier run produced. Naming
			// it as an external node is what makes reuse visible: the picture
			// shows work that was not paid for twice.
			node.Ref = a.ref
			extra.Nodes = append(extra.Nodes, agentgraph.Node{
				ID: a.ref, Kind: agentgraph.KindExternal, State: agentgraph.StateCompleted,
			})
			extra.Edges = append(extra.Edges, agentgraph.Edge{From: a.ref, To: id, Kind: agentgraph.Adopt})
		} else {
			node.Profile = specs[i].Worker.Profile
			node.Model = specs[i].Worker.Model
			node.Effort = specs[i].Worker.Effort
			node.Grant = grantOf(specs[i].Grant.ReadOnly)
		}
		workers = append(workers, node)
		for _, dep := range plan.deps[i] {
			extra.Edges = append(extra.Edges, agentgraph.Edge{
				From: fleetNodeID(parentID, dep), To: id, Kind: agentgraph.Depends,
			})
		}
	}
	delta := fanOutOpeningDelta(parentID, fmt.Sprintf("fleet(%d)", len(plan.ids)), workers)
	delta.Nodes = append(delta.Nodes, extra.Nodes...)
	delta.Edges = append(delta.Edges, extra.Edges...)
	return delta
}

// grantOf names what a run was allowed to touch. An adopted node gets none:
// nothing executed under it, and reporting one would say a claim was held.
func grantOf(readOnly bool) agentgraph.Grant {
	if readOnly {
		return agentgraph.GrantRead
	}
	return agentgraph.GrantWrite
}

// fleetQueuedDelta reports an item leaving the dependency graph for the
// scheduler queue, and the answers it will open with. A context edge is a
// separate fact from the depends edge that ordered the pair: the measurement arm
// delivers no upstream at all, and a reader has to be able to see a fleet that
// ordered its items while carrying nothing between them.
func fleetQueuedDelta(parentID string, plan fleetPlan, idx int, upstream []UpstreamResult) agentgraph.Delta {
	delta := fanOutItemQueuedDelta(fleetNodeID(parentID, idx))
	for _, up := range upstream {
		from := plan.indexOf(up.ID)
		if from < 0 {
			continue
		}
		delta.Edges = append(delta.Edges, agentgraph.Edge{
			From: fleetNodeID(parentID, from), To: fleetNodeID(parentID, idx), Kind: agentgraph.Context,
		})
	}
	return delta
}

func fleetOutcomeDelta(parentID string, state agentgraph.NodeState, results []fleetItemResult) agentgraph.Delta {
	items := make([]agentgraph.Node, 0, len(results))
	for i, r := range results {
		node := agentgraph.Node{ID: fleetNodeID(parentID, i), State: r.status, Ref: r.ref}
		if r.err != nil {
			node.Err = boundedInline(r.err.Error(), graphErrBudget)
		}
		items = append(items, node)
	}
	return fanOutOutcomeDelta(parentID, state, items)
}

func fleetItemStates(results []fleetItemResult) []agentgraph.NodeState {
	states := make([]agentgraph.NodeState, 0, len(results))
	for _, r := range results {
		states = append(states, r.status)
	}
	return states
}

// parallelItem is a parallel_tasks member as the graph sees it: what to call it
// and what it will run as. The tool takes a model and an effort per task, so two
// members of one group are not necessarily the same worker.
type parallelItem struct{ Label, Model, Effort string }

// parallelOpeningDelta declares a parallel_tasks group. Nothing orders these
// items, and the absence of an edge between two of them is the fact a reader
// needs: they ran at the same time.
func parallelOpeningDelta(parentID string, items []parallelItem) agentgraph.Delta {
	workers := make([]agentgraph.Node, 0, len(items))
	for i, item := range items {
		workers = append(workers, agentgraph.Node{
			ID: parallelNodeID(parentID, i), ParentID: parentID, Kind: agentgraph.KindWorker,
			State: agentgraph.StatePending, Label: item.Label, Grant: agentgraph.GrantRead,
			Model: item.Model, Effort: item.Effort,
		})
	}
	return fanOutOpeningDelta(parentID, fmt.Sprintf("parallel_tasks(%d)", len(items)), workers)
}

func parallelOutcomeDelta(parentID string, state agentgraph.NodeState, states []agentgraph.NodeState, refs []string, errs []error) agentgraph.Delta {
	items := make([]agentgraph.Node, 0, len(states))
	for i, itemState := range states {
		node := agentgraph.Node{ID: parallelNodeID(parentID, i), State: itemState}
		if i < len(refs) {
			node.Ref = refs[i]
		}
		if i < len(errs) && errs[i] != nil {
			node.Err = boundedInline(errs[i].Error(), graphErrBudget)
		}
		items = append(items, node)
	}
	return fanOutOutcomeDelta(parentID, state, items)
}
