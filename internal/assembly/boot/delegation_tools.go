package boot

import (
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/delegation"
)

func addDelegationTools(reg *tool.Registry, runner *delegation.Runner, store *delegation.SubagentStore, root string) {
	reg.Add(delegation.NewSpawner(runner))
	reg.Add(delegation.NewSubagentResultTool(store, root))
}
