package boot

import (
	"strings"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/command"
	"reasonix/internal/ext/extension"
	"reasonix/internal/ext/hook"
	"reasonix/internal/ext/skill"
	"reasonix/internal/session/control"
	"reasonix/internal/state/instruction"
	"reasonix/internal/state/memory"
	"reasonix/internal/state/migration"
	"reasonix/internal/state/storage"
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
	Memory                  *memory.Set
	ProjectChecks           []instruction.VerifyCheck
	ProjectSensitivePaths   []string
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

// continuesGeneration reports a build replacing a live runtime rather than
// starting one, so it can skip upgrades its predecessor already ran. Only
// PreviousSnapshot answers this — build() adopts an owner for every build, so
// an Owner check would read a first launch as a continuation and skip the
// import that launch exists to run.
func continuesGeneration(opts Options) bool {
	return opts.PreviousSnapshot != nil
}

// migrateLegacySources moves pre-v2 memory and session files into place,
// collects what an older relocation left in the previous root, and marks the
// locations an older relocation moved data to without recording anything there.
// A continued generation already did it, and rescanning on every rebuild would
// charge a window with several panes for the same disk walk once per pane.
func migrateLegacySources(opts Options, sink event.Sink) {
	if continuesGeneration(opts) {
		return
	}
	migration.MigrateLegacyMemorySources(sink)
	migration.MigrateLegacySessionSources(sink)
	migration.AdoptRelocatedStateEntries(sink)
	storage.RecordRelocatedRoots()
}

// changesModel reports a build targeting a different model than the live
// controller — which a patched subgraph keeps, so the fast path would report a
// successful switch with the old model still serving.
func changesModel(opts Options, old *control.Controller) bool {
	ref := strings.TrimSpace(opts.Model)
	return old != nil && ref != "" && ref != strings.TrimSpace(old.ModelRef())
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
