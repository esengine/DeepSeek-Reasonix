package agent

import (
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/usecap"
	"reasonix/internal/tools/builtin"
)

// GuardianToolRegistry creates a new review actor without changing the generic
// read-only filter's permission or authorized MCP selection rules.
func GuardianToolRegistry(parent *tool.Registry) *tool.Registry {
	reg := FilterReadOnlyRegistry(parent, SubagentMetaTools()...)
	forkRoleFileViews(reg, nil)
	return reg
}

func forkRoleFileViews(reg *tool.Registry, frontend *usecap.UseCapabilityTool) {
	bind := builtin.ForkFileViews(reg)
	if frontend == nil {
		if proxy, ok := reg.Get("use_capability"); ok {
			if local, ok := proxy.(*usecap.UseCapabilityTool); ok {
				frontend = local.CloneForAgent(nil, nil)
			}
		}
	}
	if frontend != nil {
		frontend.BindLocalTools(bind)
		reg.Add(frontend)
	}
}
