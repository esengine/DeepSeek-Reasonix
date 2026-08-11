package agent

import (
	"strings"

	"reasonix/internal/tool"
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
	exclude := append(SubagentMetaTools(), plannerNonResearchTools...)
	base := FilterReadOnlyRegistry(parent, exclude...)
	sub := tool.NewRegistry()
	sub.Add(NewSubmitPlanTool())
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
			if uc, ok := tl.(*UseCapabilityTool); ok {
				sub.Add(uc.CloneForAgent(nil, nil))
			} else {
				sub.Add(tl)
			}
		}
	}
	return sub
}
