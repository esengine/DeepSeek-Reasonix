package boot

import (
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/delegation"
)

// addDelegationTools registers every tool that spawns or reads a sub-agent.
// The registry exports schemas in stable name order, and this surface is
// deliberately static: profile names and result refs never enter
// provider-visible schemas, and neither reader changes between turns.
func addDelegationTools(reg *tool.Registry, taskTool *delegation.TaskTool) {
	reg.Add(taskTool)
	reg.Add(delegation.NewParallelTasksTool(taskTool, reg))
	reg.Add(delegation.NewFleetTool(taskTool))
	reg.Add(delegation.NewSubagentResultTool(taskTool))
	reg.Add(delegation.NewSubagentListTool(taskTool))
	reg.Add(delegation.NewReadOnlyTaskTool(taskTool))
}
