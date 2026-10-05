package agentgraph

import "slices"

// NodeKind separates the three things a node can be. A group is the call that
// owns a fan-out, a worker is one delegated run, and an external node is work
// this run did not do — named only because an edge points at it.
type NodeKind string

const (
	KindGroup    NodeKind = "group"
	KindWorker   NodeKind = "worker"
	KindExternal NodeKind = "external"
)

// NodeState is where one node stands. It is the node's own lifecycle and not a
// tool call's: an adopted node never ran and still holds an answer, and a
// skipped one never started because something upstream did not finish.
type NodeState string

const (
	StatePending NodeState = "pending"
	// StateQueued is ready and waiting for a session concurrency slot. It is
	// not StatePending: nothing upstream is missing, and the only thing between
	// this node and its first step is the scheduler's ceiling.
	StateQueued    NodeState = "queued"
	StateRunning   NodeState = "running"
	StateCompleted NodeState = "completed"
	StateAdopted   NodeState = "adopted"
	StateFailed    NodeState = "failed"
	StateCancelled NodeState = "cancelled"
	StateSkipped   NodeState = "skipped"
)

// Answered reports whether this state holds a final answer a dependent may
// start from: one the node produced, or one adopted in its place.
func (s NodeState) Answered() bool { return s == StateCompleted || s == StateAdopted }

// Terminal reports whether the node will not change state again.
func (s NodeState) Terminal() bool {
	return s != StatePending && s != StateQueued && s != StateRunning
}

// Grant is what a node was allowed to touch while it ran. Empty means the
// question does not apply — a group is not a run, and an adopted node never got
// one — which a bool could not say apart from "may write".
type Grant string

const (
	GrantRead  Grant = "read"
	GrantWrite Grant = "write"
)

// WaitCause is why a queued node is not running. Queued says a node is ready and
// held back; it does not say by what, and the answers have different remedies —
// two are session settings and the third is a path or an edge.
type WaitCause string

const (
	// WaitSlots is the session's total sub-agent ceiling.
	WaitSlots WaitCause = "slots"
	// WaitWriters is the session's concurrent-writer ceiling, which binds a
	// writer while total capacity is still free.
	WaitWriters WaitCause = "writers"
	// WaitClaim is a write path someone else already holds. No ceiling change
	// releases it: only disjoint paths, or an edge that orders the two.
	WaitClaim WaitCause = "claim"
)

// EdgeKind is what one edge means. The node pair alone says nothing: waiting
// for an answer and receiving one are separate facts, and an arm that orders
// two nodes while delivering nothing between them is the case a single edge
// type cannot describe.
type EdgeKind string

const (
	// Spawn is structural: To was created by From.
	Spawn EdgeKind = "spawn"
	// Depends orders: To may not start until From has answered.
	Depends EdgeKind = "depends"
	// Context carries: From's answer opened To's run.
	Context EdgeKind = "context"
	// Adopt reuses: From's existing answer stood in for running To, which
	// therefore cost nothing. From is usually outside this graph.
	Adopt EdgeKind = "adopt"
)

// Node is one participant in a run. Zero fields mean "not learned yet", which
// is what lets a producer report an outcome without restating the identity it
// declared when the node entered the graph.
type Node struct {
	ID       string    `json:"id"`
	ParentID string    `json:"parentId,omitempty"`
	Kind     NodeKind  `json:"kind,omitempty"`
	State    NodeState `json:"state,omitempty"`
	Label    string    `json:"label,omitempty"`
	Profile  string    `json:"profile,omitempty"`
	Model    string    `json:"model,omitempty"`
	Effort   string    `json:"effort,omitempty"`
	Grant    Grant     `json:"grant,omitempty"`
	// Wait is what held this node out of a slot. Like the stamps it survives the
	// run starting: the state says it is running now, this says what it waited on.
	Wait WaitCause `json:"wait,omitempty"`
	// Ref is the persisted transcript this node's answer reads back from.
	Ref string `json:"ref,omitempty"`
	// QueuedAt, StartedAt and EndedAt are unix milliseconds, zero while unknown.
	// QueuedAt to StartedAt is slot wait, which only the scheduler can measure.
	QueuedAt  int64  `json:"queuedAt,omitempty"`
	StartedAt int64  `json:"startedAt,omitempty"`
	EndedAt   int64  `json:"endedAt,omitempty"`
	Err       string `json:"err,omitempty"`
}

// Edge is one typed relation between two node ids. From may name a node outside
// this graph: an adopted answer's source is work some earlier run did.
type Edge struct {
	From string   `json:"from"`
	To   string   `json:"to"`
	Kind EdgeKind `json:"kind"`
}

// Delta is one publication of what a producer has just proven. It is additive:
// a node named again updates the one already there rather than forking into a
// second, and a repeated edge is the same edge.
type Delta struct {
	Nodes []Node `json:"nodes,omitempty"`
	Edges []Edge `json:"edges,omitempty"`
}

// Empty reports whether this delta carries nothing worth publishing.
func (d Delta) Empty() bool { return len(d.Nodes) == 0 && len(d.Edges) == 0 }

// Graph is the folded result. Nodes and edges keep first-seen order so two
// consumers fed the same deltas draw the same picture. Lookups scan rather than
// index: a graph is bounded by one call's fan-out, and a map would have to be
// rebuilt every time the value crossed a wire.
type Graph struct {
	Nodes []Node `json:"nodes,omitempty"`
	Edges []Edge `json:"edges,omitempty"`
}

// Apply folds one delta into the graph.
func (g *Graph) Apply(d Delta) {
	for _, n := range d.Nodes {
		g.upsert(n)
	}
	for _, e := range d.Edges {
		g.link(e)
	}
}

// Node returns the node with this id.
func (g *Graph) Node(id string) (Node, bool) {
	if i := g.indexOf(id); i >= 0 {
		return g.Nodes[i], true
	}
	return Node{}, false
}

func (g *Graph) indexOf(id string) int {
	return slices.IndexFunc(g.Nodes, func(n Node) bool { return n.ID == id })
}

func (g *Graph) upsert(n Node) {
	if n.ID == "" {
		return
	}
	if i := g.indexOf(n.ID); i >= 0 {
		g.Nodes[i] = merge(g.Nodes[i], n)
		return
	}
	g.Nodes = append(g.Nodes, n)
}

// merge keeps what the graph already knew wherever the update says nothing, so
// an outcome-only update cannot blank the identity an earlier delta declared.
func merge(old, update Node) Node {
	out := update
	out.ParentID = orText(update.ParentID, old.ParentID)
	out.Kind = NodeKind(orText(string(update.Kind), string(old.Kind)))
	out.State = NodeState(orText(string(update.State), string(old.State)))
	out.Label = orText(update.Label, old.Label)
	out.Profile = orText(update.Profile, old.Profile)
	out.Model = orText(update.Model, old.Model)
	out.Effort = orText(update.Effort, old.Effort)
	out.Ref = orText(update.Ref, old.Ref)
	out.Err = orText(update.Err, old.Err)
	out.Grant = Grant(orText(string(update.Grant), string(old.Grant)))
	out.Wait = WaitCause(orText(string(update.Wait), string(old.Wait)))
	out.QueuedAt = orStamp(update.QueuedAt, old.QueuedAt)
	out.StartedAt = orStamp(update.StartedAt, old.StartedAt)
	out.EndedAt = orStamp(update.EndedAt, old.EndedAt)
	return out
}

// link adds an edge once. A self-edge or a half-named one is a producer bug the
// graph refuses to record rather than render as a loop nothing can explain.
func (g *Graph) link(e Edge) {
	if e.From == "" || e.To == "" || e.Kind == "" || e.From == e.To {
		return
	}
	if !slices.Contains(g.Edges, e) {
		g.Edges = append(g.Edges, e)
	}
}

func orText(update, old string) string {
	if update != "" {
		return update
	}
	return old
}

func orStamp(update, old int64) int64 {
	if update != 0 {
		return update
	}
	return old
}
