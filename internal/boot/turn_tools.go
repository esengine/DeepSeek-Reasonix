package boot

import (
	"reasonix/internal/agent"
	"reasonix/internal/tool"
)

func registerInteractiveAgentTools(reg *tool.Registry) {
	reg.Add(agent.NewAskTool())
}
