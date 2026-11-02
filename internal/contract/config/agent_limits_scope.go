package config

import "fmt"

// agentLimits are the counts that bound how much runs at once: delegation, side
// conversations and queued prompts. A repo may not move any of them: lowering
// one is a stall, and raising the depth is unbounded cost, so the user's own
// file decides all of them.
type agentLimits struct {
	concurrency, writers, depth, sideSessions, queuedInputs, providerRetries, leaseMaxWait int
}

func holdAgentLimits(c *Config) agentLimits {
	a := c.Agent
	return agentLimits{a.MaxSubagentConcurrency, a.MaxParallelWriters, a.MaxSubagentDepth, a.MaxSideSessions, a.MaxQueuedInputs, a.MaxProviderRetries, a.WriteLeaseMaxWaitMinutes}
}

func (h heldScope) narrowAgentLimits(c *Config) {
	for _, l := range []struct {
		key  string
		now  *int
		held int
	}{
		{"agent.max_subagent_concurrency", &c.Agent.MaxSubagentConcurrency, h.limits.concurrency},
		{"agent.max_parallel_writers", &c.Agent.MaxParallelWriters, h.limits.writers},
		{"agent.max_subagent_depth", &c.Agent.MaxSubagentDepth, h.limits.depth},
		{"agent.max_side_sessions", &c.Agent.MaxSideSessions, h.limits.sideSessions},
		{"agent.max_queued_inputs", &c.Agent.MaxQueuedInputs, h.limits.queuedInputs},
		{"agent.max_provider_retries", &c.Agent.MaxProviderRetries, h.limits.providerRetries},
		{"agent.write_lease_max_wait_minutes", &c.Agent.WriteLeaseMaxWaitMinutes, h.limits.leaseMaxWait},
	} {
		if *l.now != l.held {
			c.ignoreProject(l.key, fmt.Sprint(*l.now), ProjectUserOnly)
			*l.now = l.held
		}
	}
}
