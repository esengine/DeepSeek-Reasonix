package control

import (
	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

// ContextMaintenanceSnapshot exposes current composition and the last durable
// maintenance receipt without conflating them with cumulative usage cost.
func (c *Controller) ContextMaintenanceSnapshot() agent.ContextMaintenanceSnapshot {
	if c.executor == nil {
		return agent.ContextMaintenanceSnapshot{}
	}
	return c.executor.ContextMaintenanceSnapshot()
}

// ModelVisibleMessages is what the next request would carry: the folded view
// plus host state derived for it. History() is the canonical record; the two
// answer different questions and a caller has to say which it means.
func (c *Controller) ModelVisibleMessages() []provider.Message {
	if c.executor == nil {
		return nil
	}
	return c.executor.ModelVisibleMessages()
}
