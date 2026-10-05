package boot

import (
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
)

// addTurnExitTools registers the three ways a turn ends on the model's terms: a
// decision only the user can make, an open list gated on the user, a task that
// cannot be done as specified. All stay registered every turn — the schema is
// cache-stable, and none of the three is a condition the host detects first.
func addTurnExitTools(reg *tool.Registry) {
	// Both reach the user through the call context's Asker, which interactive
	// frontends wire up (EnableInteractiveApproval). A headless run has none:
	// ask stays unresolved, await_user refuses.
	reg.Add(agent.NewAskTool())
	reg.Add(agent.NewAwaitUserTool())
	reg.Add(agent.NewConcludeBlockedTool())
}
