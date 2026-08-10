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

func (t *TaskTool) childMaxStepsForContext(ctx context.Context, requested int) int {
	if requested > 0 || t.maxSteps > 0 {
		return t.childMaxSteps(requested)
	}
	if limit, ok := runStepLimitFromContext(ctx); ok && limit.inherit {
		return childMaxStepsForParent(limit.steps, requested)
	}
	return 0
}
