package agent

import (
	"reasonix/internal/provider"
	"strings"
)

// ToolRecoveryStatistics counts evidence retained in the current session, not
// a telemetry estimate or a claim about external effects.
type ToolRecoveryStatistics struct {
	Unknown   int `json:"unknown"`
	Confirmed int `json:"confirmed"`
	Retried   int `json:"retried"`
	Rejected  int `json:"rejected"`
	Blocked   int `json:"blocked"`
}

func (a *Agent) ToolRecoveryStatistics() ToolRecoveryStatistics {
	var stats ToolRecoveryStatistics
	seen := map[string]bool{}
	for _, m := range a.Session().Snapshot() {
		if m.Role == provider.RoleTool && strings.HasPrefix(m.Content, "blocked: recovery_required:") {
			stats.Blocked++
		}
		for _, c := range m.ToolCalls {
			r := c.Recovery
			if r == nil || seen[r.Identity.AttemptID] {
				continue
			}
			seen[r.Identity.AttemptID] = true
			if unresolvedToolRecord(*r) {
				stats.Unknown++
			}
			if r.State == provider.ToolRunUserConfirmed {
				stats.Confirmed++
			}
			if r.SupersededBy != "" {
				stats.Retried++
			}
			if r.Resolution == "reject" {
				stats.Rejected++
			}
		}
	}
	return stats
}
