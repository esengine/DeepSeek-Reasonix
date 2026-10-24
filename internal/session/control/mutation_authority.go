package control

import (
	"context"

	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

// CheckMutationExecution reuses turn admission's session and runtime ownership
// checks for host work finishing after delegation. It never grants permission
// or holds a controller mutex across file coordination or callbacks.
func (c *Controller) CheckMutationExecution(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c == nil {
		return tool.Refusal{Code: "isolation.stale_execution", Message: "source runtime is unavailable"}
	}
	if err := c.ensureWriteAuthorityReady(); err != nil {
		return err
	}
	c.mu.Lock()
	retired := c.gate.closed || c.gate.rotating || c.gate.bindingPending || c.rejectDrainingGenerationLocked()
	c.mu.Unlock()
	if retired || c.PlanMode() {
		return tool.Refusal{Code: "isolation.stale_execution", Message: "source runtime is retired, changing, or read-only"}
	}
	return nil
}

// MutationExecutionValidator binds host work to this exact conversation and
// writer authority, even when /new or resume reuses the Controller object.
func (c *Controller) MutationExecutionValidator(owner string) func(context.Context) error {
	if c == nil || c.executor == nil {
		return func(context.Context) error {
			return tool.Refusal{Code: "isolation.stale_execution", Message: "source execution is unavailable"}
		}
	}
	path, session := c.SessionPath(), c.executor.Session()
	authority := session.WriteAuthority()
	return func(ctx context.Context) error {
		if err := c.CheckMutationExecution(ctx); err != nil {
			return err
		}
		if owner == "" || sessionstore.BranchID(path) != owner || c.SessionPath() != path ||
			c.executor.Session() != session || session.WriteAuthority() != authority {
			return tool.Refusal{Code: "isolation.stale_execution", Message: "source conversation or writer authority changed"}
		}
		return nil
	}
}
