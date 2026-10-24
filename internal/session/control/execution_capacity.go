package control

import "reasonix/internal/runtime/writeclaim"

// DelegationScheduler is the root owner retained by a same-session replacement.
// Limits remain frozen while old jobs and their physical requests can still run.
func (c *Controller) DelegationScheduler() *writeclaim.SubagentScheduler {
	return c.delegationScheduler
}
