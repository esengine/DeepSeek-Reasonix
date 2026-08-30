package agent

import (
	"context"

	"reasonix/internal/checkpoint"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/runtimepolicy"
)

// subagentOptions is the single construction point for the run options every
// sub-agent spawned through this tool shares (task, read_only_task, and
// parallel_tasks children). Compaction, language preferences, and depth limits
// must stay uniform across those paths — add new fields here, not at call sites.
func (t *TaskTool) subagentOptions(ctx context.Context, maxSteps int, pricing *provider.Pricing, ctxWin, childDepth int, recoveryTaskID string, mutationObserver *checkpoint.MutationObserver) Options {
	opts := Options{
		MaxSteps:                 maxSteps,
		MaxOutputTokens:          childOutputBudgetFrom(ctx),
		Temperature:              t.temperature,
		Pricing:                  pricing,
		QuoteContext:             t.quoteContext,
		UsageSource:              event.UsageSourceSubagent,
		Gate:                     t.gate,
		ContextWindow:            ctxWin,
		RecentKeep:               t.recentKeep,
		CompactRatio:             t.compactRatio,
		ArchiveDir:               t.archiveDir,
		KeepPolicy:               t.keepPolicy,
		ResponseLanguage:         ResponseLanguageFromContext(ctx),
		ReasoningLanguage:        ReasoningLanguageFromContext(ctx),
		SubagentDepth:            childDepth,
		MaxSubagentDepth:         t.maxDepth(),
		Ablation:                 t.ablation,
		WorkspaceLease:           t.workspaceLease,
		RecoveryGate:             t.recoveryGate,
		RecoveryAgentID:          "subagent",
		RecoveryTaskID:           recoveryTaskID,
		MutationObserver:         mutationObserver,
		WriteRoots:               t.writeRoots,
		DisableWriteAccessExpand: true,
		WriteWorkspaceRoot:       t.workspaceRoot,
	}
	// Writer children inherit the parent turn's frozen risk and closure floors.
	// The parent publishes its policy into the run context; a child that never
	// received it (direct unit construction) keeps its own derived policy.
	if inherited, ok := runtimepolicy.InheritedFromContext(ctx); ok {
		copy := inherited
		opts.InheritedExecution = &copy
	} else if constraints, ok := runtimepolicy.FromContext(ctx); ok {
		opts.InheritedExecution = &runtimepolicy.InheritedExecutionContext{Constraints: constraints}
	}
	return opts
}
