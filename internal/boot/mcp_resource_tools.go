package boot

import (
	"reasonix/internal/plugin"
	"reasonix/internal/tool"
)

func addMCPResourceTools(reg *tool.Registry, host *plugin.Host, specs []plugin.Spec) {
	if reg == nil || host == nil || len(specs) == 0 {
		return
	}
	for _, candidate := range plugin.NewMCPResourceTools(host) {
		reg.Add(candidate)
	}
}
