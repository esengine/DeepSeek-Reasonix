package agent

import "context"

// childMaxSteps resolves a sub-agent budget; an explicit request always wins.
func (t *TaskTool) childMaxSteps(requested int) int {
	return childMaxStepsForParent(t.maxSteps, requested)
}

func childMaxStepsForParent(parent, requested int) int {
	if requested > 0 {
		return requested
	}
	if parent <= 0 {
		return 0
	}
	return max(parent/2, 5)
}

func (t *TaskTool) childMaxStepsForContext(_ context.Context, requested int) int {
	return t.childMaxSteps(requested)
}

func (t *TaskTool) childMaxStepsForSpec(ctx context.Context, spec *ProfileExecSpec) (context.Context, int) {
	applyReviewBudget(spec, t.reviewMaxSteps)
	fillChildFacts(ctx, spec)
	if spec == nil {
		return ctx, t.childMaxStepsForContext(ctx, 0)
	}
	ctx = withChildOutputBudget(ctx, spec.Sched.MaxOutputTokens)
	return ctx, t.childMaxStepsForContext(ctx, spec.Sched.MaxSteps)
}
