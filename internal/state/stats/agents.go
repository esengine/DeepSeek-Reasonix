package stats

import (
	"slices"
	"strings"

	"reasonix/internal/state/usagecatalog"
)

// AgentUsage is what one team agent's seats spent within the range. Calls made
// off the team carry no agent and are not listed, so the shares need not add
// up to the whole.
type AgentUsage struct {
	Agent    string  `json:"agent"`
	Tokens   int64   `json:"tokens"`
	Requests int64   `json:"requests"`
	Percent  float64 `json:"percent"` // of every token in the range, 0..100
}

// agentTally sums records by the agent that made them.
type agentTally map[string]*AgentUsage

func (t agentTally) add(rec record, requests int) {
	if rec.Agent == "" || rec.Turn {
		return
	}
	a := t[rec.Agent]
	if a == nil {
		a = &AgentUsage{Agent: rec.Agent}
		t[rec.Agent] = a
	}
	a.Tokens += int64(rec.Total)
	a.Requests += int64(max(requests, 0))
}

func (t agentTally) sorted(total int64) []AgentUsage {
	out := make([]AgentUsage, 0, len(t))
	for _, a := range t {
		out = append(out, *a)
	}
	return withShares(out, total)
}

func agentsFromRollups(rows []usagecatalog.AgentRollup, total int64) []AgentUsage {
	out := make([]AgentUsage, 0, len(rows))
	for _, r := range rows {
		out = append(out, AgentUsage{Agent: r.Agent, Tokens: r.Total, Requests: r.Requests})
	}
	return withShares(out, total)
}

func withShares(out []AgentUsage, total int64) []AgentUsage {
	slices.SortFunc(out, func(a, b AgentUsage) int {
		if a.Tokens != b.Tokens {
			if a.Tokens > b.Tokens {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Agent, b.Agent)
	})
	if total > 0 {
		for i := range out {
			out[i].Percent = float64(out[i].Tokens) / float64(total) * 100
		}
	}
	return out
}
