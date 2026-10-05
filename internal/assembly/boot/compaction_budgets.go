package boot

import (
	"reasonix/internal/contract/config"
	"reasonix/internal/runtime/agent"
)

// compactionBudgets carries the user's fold bounds to every agent the boot
// assembles, parent and sub-agent alike, so a setting does not silently stop
// applying one delegation down.
func compactionBudgets(cfg *config.Config) agent.CompactionBudgets {
	if cfg == nil {
		return agent.CompactionBudgets{}
	}
	return agent.CompactionBudgets{
		UserTurnKeepTokens:     cfg.Agent.UserTurnKeepTokens,
		FirstTurnPinTokens:     cfg.Agent.FirstTurnPinTokens,
		CheckpointCeilingRatio: cfg.Agent.CheckpointCeilingRatio,
		ContextSoftLimitTokens: cfg.Agent.ContextSoftLimitTokens,
	}
}
