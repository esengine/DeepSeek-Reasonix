package boot

import (
	"reasonix/internal/command"
	"reasonix/internal/extension"
	"reasonix/internal/hook"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

// ReusedAssembly holds rediscovery-free inputs for narrow/no-op rebuilds.
// Populated on successful BuildRuntime so the next RebuildFrom can skip
// skill/command/hook rediscovery and snapshot re-freeze when the plan allows.
type ReusedAssembly struct {
	SystemPrompt            string
	Skills                  []skill.Skill
	Commands                []command.Command
	Hooks                   []hook.ResolvedHook
	Registry                *tool.Registry
	ImplicitSkillInvocation bool
}

// shouldReuseDiscovery reports whether rediscovery of skills/commands/hooks
// can be skipped for this rebuild plan. Provider-only and MCP-only rebuilds
// do not change skill/command/hook discovery either — only the live backend.
func shouldReuseDiscovery(plan *extension.RuntimePlan) bool {
	if plan == nil {
		return false
	}
	switch plan.Kind {
	case extension.SubgraphNone,
		extension.SubgraphInterceptorOnly,
		extension.SubgraphUIOnly,
		extension.SubgraphProviderOnly,
		extension.SubgraphMCPOnly:
		return true
	default:
		return false
	}
}

// shouldReuseSnapshot reports whether the previous RuntimeSnapshot body can be
// re-published with a new generation (provider-visible prefix unchanged).
func shouldReuseSnapshot(plan *extension.RuntimePlan) bool {
	if plan == nil {
		return false
	}
	return plan.IsNoOp() || !plan.MayChangePrefix()
}

// shouldSkipPromptStrategy is true when system_prompt.build must not re-run.
func shouldSkipPromptStrategy(plan *extension.RuntimePlan) bool {
	return shouldReuseSnapshot(plan)
}
