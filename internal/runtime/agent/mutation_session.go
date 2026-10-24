package agent

import (
	"context"

	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

// CheckMutationSession checks the actual delegated agent, independently of the
// unused root executor in its environment's controller. It grants no paths.
func (a *Agent) CheckMutationSession(ctx context.Context, session *sessionstore.Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a == nil || session == nil || a.Session() != session || a.role.readOnlyExecution || a.PlanningPhase() {
		return tool.Refusal{Code: "isolation.stale_execution", Message: "delegated source session is unavailable, replaced, or read-only"}
	}
	authority := session.WriteAuthority()
	if (session.WriteAuthorityRequired() && authority == nil) || (authority != nil && !authority.Valid()) {
		return tool.Refusal{Code: "isolation.stale_execution", Message: "delegated session writer authority is no longer valid"}
	}
	return nil
}
