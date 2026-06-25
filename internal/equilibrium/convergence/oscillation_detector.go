package convergence

import (
	"sort"

	globalstate "reasonix/internal/equilibrium/global_state"
)

func DetectOscillation(samples []globalstate.DecisionSample) globalstate.OscillationReport {
	report := globalstate.OscillationReport{Severity: "low"}
	if len(samples) < 3 {
		return report
	}
	transitions := 0
	for i := 1; i < len(samples); i++ {
		if samples[i].Action != samples[i-1].Action {
			transitions++
		}
	}
	report.Frequency = float64(transitions) / float64(len(samples)-1)
	switch {
	case report.Frequency >= 0.72:
		report.Severity = "high"
	case report.Frequency >= 0.45:
		report.Severity = "medium"
	default:
		report.Severity = "low"
	}
	report.AffectedNodes = affectedNodes(samples)
	return report
}

func affectedNodes(samples []globalstate.DecisionSample) []string {
	counts := map[string]int{}
	for _, sample := range samples {
		for _, influence := range sample.NodeInfluence {
			if influence.Share >= 0.18 {
				counts[influence.NodeID]++
			}
		}
	}
	type nodeCount struct {
		id    string
		count int
	}
	nodes := make([]nodeCount, 0, len(counts))
	for id, count := range counts {
		nodes = append(nodes, nodeCount{id: id, count: count})
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].count == nodes[j].count {
			return nodes[i].id < nodes[j].id
		}
		return nodes[i].count > nodes[j].count
	})
	out := make([]string, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node.id)
		if len(out) >= 5 {
			break
		}
	}
	return out
}
