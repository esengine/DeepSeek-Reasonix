package boot

import (
	"context"
	"reasonix/internal/contract/tool"
	"strings"

	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/skill"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/runtime/langpref"
)

// delegationInputs is what a sub-agent inherits from the executor: its model,
// its step ceiling, and the session resources it runs inside.
type delegationInputs struct {
	opts         Options
	sub          subagentConfig
	exec         provider.Provider
	entry        *config.ProviderEntry
	modelName    string
	root         string
	maxSteps     int
	delivery     bool
	store        *delegation.SubagentStore
	session      sessionRuntime
	bashEnforced func() bool
	extraRoots   []string
	// childTools names what a child gets from its own model beyond the parent's.
	childTools func(modelRef string) []tool.Tool
}

func (w roleWiring) delegation(in delegationInputs) (*delegation.Runner, *skillSubagents) {
	runner := delegation.NewRunner(delegation.RunnerOptions{
		Registry:      w.reg,
		Provider:      in.exec,
		Price:         in.entry.Price,
		ContextWindow: in.entry.ContextWindow,
		Store:         in.store,
		WorkspaceRoot: in.root,
		MaxDepth:      in.sub.maxDepth,
		MaxSteps:      in.maxSteps,
		SystemPrompt:  delegation.DefaultReadOnlyTaskSystemPrompt,
		Resolve:       in.sub.resolveProvider,
		Identity:      in.sub.identity,
		RunOptions:    w.skillRunOptions(in),
	})
	if !in.opts.Ablation.Off(ablation.Subagent) {
		addDelegationTools(w.reg, runner, in.store, in.root)
	}
	return runner, &skillSubagents{
		root:            in.root,
		cfg:             w.cfg,
		registry:        w.reg,
		runner:          runner,
		scheduler:       in.sub.scheduler,
		provider:        in.exec,
		entry:           in.entry,
		maxDepth:        in.sub.maxDepth,
		maxSteps:        in.maxSteps,
		resolveProvider: in.sub.resolveProvider,
		identity:        in.sub.identity,
		inheritedFor:    in.sub.inheritedFor,
		runOptions:      w.skillRunOptions(in),
	}
}

// skillRunOptions is the one place skill sub-agent options are built, so the
// read-only and writer-capable runners cannot drift on compaction or language.
func (w roleWiring) skillRunOptions(in delegationInputs) func(context.Context, int, *provider.Pricing, int, int) agent.Options {
	return func(sctx context.Context, steps int, price *provider.Pricing, ctxWin, childDepth int) agent.Options {
		return agent.Options{
			MaxSteps:            steps,
			Temperature:         w.cfg.Agent.Temperature,
			Pricing:             price,
			UsageSource:         event.UsageSourceSubagent,
			Gate:                w.gate,
			CheckTargetAccess:   w.checkTargetAccess,
			ContextWindow:       ctxWin,
			CompactionBudgets:   compactionBudgets(w.cfg),
			CompactRatio:        w.cfg.Agent.CompactRatio,
			ArchiveDir:          w.roots.ArchiveDir(),
			ResponseLanguage:    langpref.ResponseLanguageFromContext(sctx),
			ReasoningLanguage:   langpref.ReasoningLanguageFromContext(sctx),
			SubagentDepth:       childDepth,
			MaxSubagentDepth:    in.sub.maxDepth,
			ProviderRetries:     agent.ProviderRetriesFromConfig(w.cfg.Agent.MaxProviderRetries),
			DeliveryProfile:     in.delivery,
			Ablation:            in.opts.Ablation,
			WorkspaceLease:      in.session.lease,
			ExtraWorkspaceRoots: in.extraRoots,
			Hooks:               w.hooks.ForRole(skillSubagentRole(sctx)),
		}
	}
}

// skillSubagentRole names a skill child's hook session after the call that
// spawned it, the way the task tool names its children.
func skillSubagentRole(ctx context.Context) string {
	if callID, _, _, ok := agent.CallContext(ctx); ok && strings.TrimSpace(callID) != "" {
		return "subagent:" + strings.TrimSpace(callID)
	}
	return "subagent"
}

// skillProfile names the model and effort a skill's sub-agent overrides, or
// nil when it inherits both.
func skillProfile(cfg *config.Config, inheritedFor func(string) string) skill.ProfileResolver {
	return func(sk skill.Skill) *event.Profile {
		model, effort := subagentModelRef(cfg, sk), subagentEffortRef(cfg, sk, inheritedFor)
		if model == "" && effort == "" {
			return nil
		}
		return &event.Profile{Model: model, Effort: effort}
	}
}
