package control

import "reasonix/internal/contract/planmode"

// Where the controller reads and moves the plan lifecycle, and the one place
// that projects it back into the legacy on/off flag the wire still carries.

// plan returns the lifecycle the controller and its agents share, creating it
// on first use. One runtime is the point: an agent with its own would judge
// stale work against an epoch the approval never moved.
func (c *Controller) plan() *planmode.Runtime {
	if r := c.planRuntime.Load(); r != nil {
		return r
	}
	fresh := planmode.NewRuntime()
	if c.planRuntime.CompareAndSwap(nil, fresh) {
		return fresh
	}
	return c.planRuntime.Load()
}

// sharePlanRuntime hands the lifecycle to whatever is running turns. An adopter
// then reads the shared state directly and must not also be told an on/off
// flag: the flag would move the state it is already reading. Anything that
// cannot adopt gets the legacy projection instead, and loses only the shared
// epoch — and with it stale-call rejection.
func (c *Controller) sharePlanRuntime() {
	r := c.plan()
	if adopter, ok := c.runner.(interface {
		AdoptPlanRuntime(*planmode.Runtime)
	}); ok {
		adopter.AdoptPlanRuntime(r)
		return
	}
	if c.executor != nil {
		c.executor.AdoptPlanRuntime(r)
		return
	}
	if setter, ok := c.runner.(interface{ SetPlanMode(bool) }); ok {
		setter.SetPlanMode(c.PlanMode())
	}
}

// PlanMode reports whether outgoing turns currently receive the plan-mode
// marker. It is the legacy projection of the lifecycle for wire and UI
// compatibility — Executing is inside the workflow but past the marker, and
// answers false the way the old flag did once a plan was approved. Nothing in
// the kernel decides on this; the kernel reads PlanPhase.
func (c *Controller) PlanMode() bool {
	switch c.plan().State().Phase {
	case planmode.Planning, planmode.AwaitingApproval:
		return true
	}
	return false
}

// SetPlanMode flips the executor's plan-first workflow flag without touching the
// cache-stable system/tool prefix, and remembers the state so Compose can prepend
// the plan-mode marker to outgoing user turns.
func (c *Controller) SetPlanMode(v bool) {
	c.applyPlanMode(v)
}

func (c *Controller) applyPlanMode(v bool) {
	c.plan().SetActive(v)
	c.sharePlanRuntime()
}

// PlanPhase reports where the run sits in the plan lifecycle.
func (c *Controller) PlanPhase() planmode.Phase { return c.plan().State().Phase }
