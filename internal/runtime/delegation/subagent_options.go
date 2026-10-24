package delegation

import (
	"context"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/langpref"
	"reasonix/internal/state/checkpoint"
	"reasonix/internal/tools/jobs"
)

// subagentOptions is the single construction point for the run options every
// sub-agent spawned through this tool shares (task, read_only_task, and
// parallel_tasks children). Compaction, language preferences, and depth limits
// must stay uniform across those paths — add new fields here, not at call sites.
func (t *TaskTool) subagentOptions(ctx context.Context, maxSteps int, pricing *provider.Pricing, ctxWin, childDepth int, recoveryTaskID string, mutationObserver *checkpoint.MutationObserver) agent.Options {
	opts := agent.Options{
		ProcessSandbox:      t.processSandbox,
		CheckEnvironment:    t.checkEnvironment,
		MaxSteps:            maxSteps,
		ProviderRetries:     t.providerRetries,
		Temperature:         t.temperature,
		Pricing:             pricing,
		UsageSource:         event.UsageSourceSubagent,
		Gate:                t.gate,
		CheckTargetAccess:   t.checkTargetAccess,
		ContextWindow:       ctxWin,
		RecentKeep:          t.recentKeep,
		CompactionBudgets:   t.budgets,
		CompactRatio:        t.compactRatio,
		ArchiveDir:          t.archiveDir,
		KeepPolicy:          t.keepPolicy,
		ResponseLanguage:    langpref.ResponseLanguageFromContext(ctx),
		ReasoningLanguage:   langpref.ReasoningLanguageFromContext(ctx),
		SubagentDepth:       childDepth,
		MaxSubagentDepth:    t.maxDepth(),
		DeliveryProfile:     t.deliveryProfile,
		Ablation:            t.ablation,
		WorkspaceLease:      t.workspaceLease,
		MutationCoordinator: t.mutations,
		RecoveryGate:        t.recoveryGate,
		RecoveryAgentID:     "subagent",
		RecoveryTaskID:      recoveryTaskID,
		MutationObserver:    mutationObserver,
	}
	if t.hooksForRole != nil {
		opts.Hooks = t.hooksForRole(recoveryTaskID)
	}
	if t.mutations != nil {
		opts.ExecutionCapacity = t.scheduler.PhaseCapacity()
	}
	opts.Jobs, _ = ctx.Value(executionJobsKey{}).(*jobs.Manager)
	return t.withExtraRoots(opts)
}
