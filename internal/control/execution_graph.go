package control

import (
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/agentgraph"
	"reasonix/internal/execgraph"
	"reasonix/internal/execjournal"
)

// ExecutionGraphSnapshot is the run graph as this session's durable facts
// justify it, recomputed on demand. It is the authority a reader starts from;
// the delta stream is a low-latency projection of the same facts, not a history
// to replay into a state.
type ExecutionGraphSnapshot struct {
	Graph agentgraph.Graph `json:"graph"`
	// Interruptions are carried beside the graph rather than folded into a
	// state: the vocabulary has no word for work whose owner is gone.
	Interruptions []ExecutionInterruption `json:"interruptions,omitempty"`
	// IdentityUnknown names the executions whose worker layer was never
	// recorded. Their model and effort read empty for a different reason than
	// an inheritance does, and showing both alike claims an unmade observation.
	IdentityUnknown []string `json:"identityUnknown,omitempty"`
}

// ExecutionInterruption is one execution the host found open with nobody
// running it. Kind says whether it had reached a slot, which is the difference
// between work that may be half-done and work that never began.
type ExecutionInterruption struct {
	Execution string `json:"execution"`
	Kind      string `json:"kind"`
}

// ExecutionGraph rebuilds the run graph from what survived. It reads: the
// journal for how each delegation entered orchestration, the sub-agent store
// for what happened to the ones that ran, and this process's own claims for
// what is still live. Nothing is emitted — a caller that wants to tell clients
// asks them to read this, rather than replaying transitions that already ended.
func (c *Controller) ExecutionGraph() ExecutionGraphSnapshot {
	if c == nil {
		return ExecutionGraphSnapshot{}
	}
	path := c.SessionPath()
	if strings.TrimSpace(path) == "" {
		return ExecutionGraphSnapshot{}
	}
	history := execjournal.History(path)
	rebuilt := execgraph.Rebuild(
		history,
		c.executionChildren(path, history),
		func(id string) bool { return execjournal.Live(path, id) },
	)
	out := ExecutionGraphSnapshot{Graph: rebuilt.Graph, IdentityUnknown: rebuilt.LegacyIdentity}
	for _, i := range rebuilt.Interrupted {
		kind := execjournal.InterruptedBeforeStart
		if i.Started {
			kind = execjournal.InterruptedDuringExecution
		}
		out.Interruptions = append(out.Interruptions, ExecutionInterruption{Execution: i.Execution, Kind: kind})
	}
	return out
}

// executionChildren maps the store's records onto the fold's input, on a join
// key that is resolved rather than assumed. An unreadable store, and a record
// nothing places, both leave outcomes unknown rather than borrowed.
func (c *Controller) executionChildren(path string, history []execjournal.Entry) []execgraph.ChildOutcome {
	artifacts, err := agent.ListSubagentsByParent(c.sessionDir, parentSessionOf(path))
	if err != nil {
		return nil
	}
	opened := make(map[string]bool, len(history))
	for _, e := range history {
		opened[e.ID] = true
	}
	out := make([]execgraph.ChildOutcome, 0, len(artifacts))
	for _, a := range artifacts {
		identity := agent.ResolveExecutionIdentity(a.Meta, func(id string) bool { return opened[id] })
		out = append(out, execgraph.ChildOutcome{
			Execution: identity.Execution, Ref: a.Ref, Status: string(a.Meta.Status),
		})
	}
	return out
}

// parentSessionOf is the id a child records for its parent: the transcript's
// stem, which is how the store's records join the journal's executions.
func parentSessionOf(path string) string {
	base := path
	if i := strings.LastIndexAny(base, "/\\"); i >= 0 {
		base = base[i+1:]
	}
	return strings.TrimSuffix(base, ".jsonl")
}
