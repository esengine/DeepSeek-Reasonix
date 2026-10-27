package agent

import (
	"reasonix/internal/runtime/usecap"
	"strings"

	"reasonix/internal/contract/tool"
)

// ask is deliberately absent: a planner that needs a user-owned decision asks
// for it with the real tool, so the answer shapes the plan instead of being
// stapled onto a finished one.
var plannerNonResearchTools = []string{
	"bash_output",
	"complete_step",
	"slash_command",
	"todo_write",
	"wait",
}

// PlannerToolRegistry returns read-only research tools, submit_plan, and an
// isolated use_capability proxy. Direct MCP schemas and selected workflow tools
// stay hidden. submit_plan is added only here, so the planner gaining a
// structured exit leaves the executor's cache-stable tool prefix untouched.
func PlannerToolRegistry(parent *tool.Registry) *tool.Registry {
	return PlannerToolRegistryWithFrontend(parent, nil)
}

// PlannerToolRegistryWithFrontend keeps a fresh role frontend's ledger and audit
// while binding its deferred reads to the planner's direct file tools.
// The supplied frontend must not have been published to another agent.
func PlannerToolRegistryWithFrontend(parent *tool.Registry, frontend *usecap.UseCapabilityTool) *tool.Registry {
	exclude := append(SubagentMetaTools(), plannerNonResearchTools...)
	base := FilterReadOnlyRegistry(parent, exclude...)
	sub := tool.NewRegistry()
	sub.CopyDisabledMCPFrom(parent)
	sub.Add(NewSubmitPlanTool())
	sub.Add(NewConcludeNoChangesTool())
	if base != nil {
		for _, name := range base.Names() {
			if name == "use_capability" || strings.HasPrefix(name, tool.MCPNamePrefix) {
				continue
			}
			if tl, ok := base.Get(name); ok {
				sub.Add(tl)
			}
		}
	}
	if parent != nil {
		if tl, ok := parent.Get("use_capability"); ok {
			sub.Add(tl)
		}
	}
	forkRoleFileViews(sub, frontend)
	return sub
}
