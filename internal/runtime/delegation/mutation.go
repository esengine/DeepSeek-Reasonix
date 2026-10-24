package delegation

import "reasonix/internal/state/mutation"

func (t *TaskTool) WithMutationCoordinator(c *mutation.Coordinator) *TaskTool {
	t.mutations = c
	return t
}
