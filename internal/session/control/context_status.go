package control

import (
	"context"
	"time"

	"reasonix/internal/contract/provider"
	"reasonix/internal/model/billing"
	"reasonix/internal/runtime/agent"
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

// Balance reads the active provider's wallet. One declaring no balance_url
// reads back unconfigured rather than failed: "there is no wallet here" and
// "the wallet did not answer" are opposite answers to why there is no number.
func (c *Controller) Balance(ctx context.Context) billing.Reading {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return c.balance.Read(ctx)
}
