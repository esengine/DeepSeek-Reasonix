package agent

import "context"

// Runner carries out one task turn. Both Agent (single model) and Coordinator
// (two-model) satisfy it, so the CLI stays agnostic to which is in use.
type Runner interface {
	Run(ctx context.Context, input string) error
}

// PlannerPlanApprover lets hosts bind a planner-authored approval request to
// their native approval UI without making the agent package depend on control.
type PlannerPlanApprover interface {
	RunWithPlannerApproval(ctx context.Context, plan string, run func(context.Context) error) error
}
