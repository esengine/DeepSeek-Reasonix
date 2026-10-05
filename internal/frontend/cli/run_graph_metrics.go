package cli

import "reasonix/internal/contract/agentgraph"

// FanoutMetrics prices what a run's fan-outs bought. It is folded from the run
// graph the kernel publishes, because the timestamps carried there are the only
// record of a wait no participant except the scheduler is able to observe.
type FanoutMetrics struct {
	Groups  int `json:"fanout_groups,omitempty"`
	Workers int `json:"fanout_workers,omitempty"`
	// Adopted counts members that stood in a finished answer instead of running.
	Adopted int `json:"fanout_adopted,omitempty"`
	// WallMs is what the run waited on fan-outs: the sum of each group's own
	// span, so groups that ran one after another add up and members do not.
	WallMs int64 `json:"fanout_wall_ms,omitempty"`
	// WorkMs is the sum of every member's run time — the same work one at a
	// time. WorkMs over WallMs is the speed-up the graph actually bought.
	WorkMs int64 `json:"fanout_work_ms,omitempty"`
	// CriticalPathMs is the heaviest depends-ordered chain of member run times:
	// the floor this shape allows. WallMs above it is scheduling loss.
	CriticalPathMs int64 `json:"fanout_critical_path_ms,omitempty"`
	// SlotWaitMs sums started − queued for members a session ceiling held back:
	// the wait that raising max_subagent_concurrency or max_parallel_writers
	// would have shortened. Each node names which of the two it was.
	SlotWaitMs int64 `json:"fanout_slot_wait_ms,omitempty"`
	// ClaimWaitMs is that same wait for members a write path someone else held
	// kept out. No ceiling releases it — only disjoint paths, or an edge that
	// orders the pair — so adding it to the figure above would misdirect.
	ClaimWaitMs int64 `json:"fanout_claim_wait_ms,omitempty"`
}

// fanoutMetricsOf folds a run graph into the scalars. A run that held no
// fan-out returns nil rather than zeros: a single-agent arm has nothing to
// price, and a row of zeros would read as a fan-out that bought nothing.
func fanoutMetricsOf(g agentgraph.Graph) *FanoutMetrics {
	members := map[string][]agentgraph.Node{}
	for _, n := range g.Nodes {
		if n.ParentID != "" {
			members[n.ParentID] = append(members[n.ParentID], n)
		}
	}
	var out FanoutMetrics
	for _, group := range g.Nodes {
		if group.Kind != agentgraph.KindGroup {
			continue
		}
		out.Groups++
		out.WallMs += nodeSpan(group)
		out.CriticalPathMs += criticalPathMs(members[group.ID], g.Edges)
		for _, m := range members[group.ID] {
			out.Workers++
			if m.State == agentgraph.StateAdopted {
				out.Adopted++
			}
			out.WorkMs += nodeSpan(m)
			if m.Wait == agentgraph.WaitClaim {
				out.ClaimWaitMs += slotWaitMs(m)
			} else {
				out.SlotWaitMs += slotWaitMs(m)
			}
		}
	}
	if out.Groups == 0 {
		return nil
	}
	return &out
}

// nodeSpan is a node's own run time, zero unless both stamps arrived: an
// adopted member never ran, and a run killed mid-flight has no end.
func nodeSpan(n agentgraph.Node) int64 {
	if n.StartedAt == 0 || n.EndedAt <= n.StartedAt {
		return 0
	}
	return n.EndedAt - n.StartedAt
}

func slotWaitMs(n agentgraph.Node) int64 {
	if n.QueuedAt == 0 || n.StartedAt <= n.QueuedAt {
		return 0
	}
	return n.StartedAt - n.QueuedAt
}

// criticalPathMs is the heaviest chain of member run times along depends edges.
// The kernel rejects cycles at preflight, but a killed run leaves a partial
// graph, so the walk guards the path it is on rather than trusting that.
func criticalPathMs(members []agentgraph.Node, edges []agentgraph.Edge) int64 {
	inside := make(map[string]agentgraph.Node, len(members))
	for _, m := range members {
		inside[m.ID] = m
	}
	deps := map[string][]string{}
	for _, e := range edges {
		if e.Kind != agentgraph.Depends {
			continue
		}
		if _, ok := inside[e.To]; !ok {
			continue
		}
		if _, ok := inside[e.From]; ok {
			deps[e.To] = append(deps[e.To], e.From)
		}
	}
	done := map[string]int64{}
	busy := map[string]bool{}
	var walk func(string) int64
	walk = func(id string) int64 {
		if known, ok := done[id]; ok {
			return known
		}
		if busy[id] {
			return 0
		}
		busy[id] = true
		var upstream int64
		for _, from := range deps[id] {
			upstream = max(upstream, walk(from))
		}
		delete(busy, id)
		done[id] = upstream + nodeSpan(inside[id])
		return done[id]
	}
	var longest int64
	for _, m := range members {
		longest = max(longest, walk(m.ID))
	}
	return longest
}
