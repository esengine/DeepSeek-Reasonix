package readcoord

import "reasonix/internal/tool"

// Associate runs after the reader captured its source. It reuses a requirement
// on that exact source without reopening completed coverage or resetting budgets.
// The caller owns run/session isolation and serializes this with Observe.
func (c *Coordinator) Associate(env tool.ReadResultEnvelope) string {
	if env.Source.Snapshot == "" || env.Source.Identity == "" {
		return env.ReadID
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var selected *Obligation
	for _, ob := range c.byKey {
		if ob.Source != env.Source || ob.Stop != nil || ob.State == StateCancelled {
			continue
		}
		if ob.Key == env.ReadID {
			selected = ob
			break
		}
		if selected == nil || ob.Key < selected.Key {
			selected = ob
		}
	}
	if selected == nil {
		return env.ReadID
	}
	req := requirementFor(env)
	if req.WholeFile {
		selected.Requirement = req
	} else if !selected.Requirement.WholeFile && req.Intent == tool.ReadIntentRange {
		selected.Requirement.Intent = tool.ReadIntentRange
		selected.Requirement.Ranges = Normalize(append(selected.Requirement.Ranges, req.Ranges...))
	}
	if selected.State == StateSatisfied && evaluate(selected, env) != StateSatisfied {
		selected.State = StateFetching
	}
	return selected.Key
}
