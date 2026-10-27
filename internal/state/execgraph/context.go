package execgraph

import "reasonix/internal/contract/agentgraph"

func recoverContextEdges(g *agentgraph.Graph, children []ChildOutcome) {
	byRef := map[string]string{}
	for _, c := range children {
		if c.Ref != "" && c.Execution != "" {
			if previous, seen := byRef[c.Ref]; seen && previous != c.Execution {
				byRef[c.Ref] = ""
			} else if !seen {
				byRef[c.Ref] = c.Execution
			}
		}
	}
	for _, c := range children {
		if c.Execution == "" {
			continue
		}
		for _, ref := range c.ContextRefs {
			if ref == "" {
				continue
			}
			from := byRef[ref]
			if from == "" {
				from = ref
				g.Apply(agentgraph.Delta{Nodes: []agentgraph.Node{{ID: from, Kind: agentgraph.KindExternal}}})
			}
			g.Apply(agentgraph.Delta{Edges: []agentgraph.Edge{{From: from, To: c.Execution, Kind: agentgraph.Context}}})
		}
	}
}

func recoverGroups(g *agentgraph.Graph) {
	for i, node := range g.Nodes {
		if node.Kind != agentgraph.KindGroup {
			continue
		}
		state := agentgraph.StateCompleted
		unknown, active, count := false, false, 0
		for _, child := range g.Nodes {
			if child.ParentID != node.ID {
				continue
			}
			count++
			switch child.State {
			case "":
				unknown = true
			case agentgraph.StateRunning, agentgraph.StatePending, agentgraph.StateQueued:
				active = true
			case agentgraph.StateFailed:
				state = agentgraph.StateFailed
			case agentgraph.StateCancelled, agentgraph.StateSkipped:
				if state != agentgraph.StateFailed {
					state = agentgraph.StateCancelled
				}
			}
		}
		if count == 0 || unknown {
			state = ""
		} else if active {
			state = agentgraph.StateRunning
		}
		g.Nodes[i].State = state
	}
}
