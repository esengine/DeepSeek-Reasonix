package boot

import (
	"reasonix/internal/agent"
	"reasonix/internal/tool"
)

func registerInteractiveAgentTools(reg *tool.Registry) {
	reg.Add(agent.NewAskTool())
	// Finish is top-level only; subagent and planner registries strip it.
	reg.Add(agent.NewFinishTool())
}
