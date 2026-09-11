package control

import "reasonix/internal/agent"

// ContextMaintenanceSnapshot exposes current composition and the last durable
// maintenance receipt without conflating them with cumulative usage cost.
func (c *Controller) ContextMaintenanceSnapshot() agent.ContextMaintenanceSnapshot {
	if c.executor == nil {
		return agent.ContextMaintenanceSnapshot{}
	}
	return c.executor.ContextMaintenanceSnapshot()
}
