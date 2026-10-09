package boot

import (
	"context"
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
}

// delegation builds the task tool and the skill sub-agent runner. Wired after
// built-ins and MCP, so a child inherits the full tool set minus task itself.
// The capability runtime does not exist yet; the caller binds it later.
func (w roleWiring) delegation(in delegationInputs) (*delegation.TaskTool, *skillSubagents) {
	var taskTool *delegation.TaskTool
	if !in.opts.Ablation.Off(ablation.Subagent) {
		taskTool = w.taskTool(in)
		addDelegationTools(w.reg, taskTool)
	}
	return taskTool, &skillSubagents{
		root:            in.root,
		cfg:             w.cfg,
		registry:        w.reg,
		tasks:           taskTool,
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

func (w roleWiring) taskTool(in delegationInputs) *delegation.TaskTool {
	return delegation.NewTaskToolWithOptions(delegation.TaskToolOptions{
		Provider:          in.exec,
		Pricing:           in.entry.Price,
		ParentRegistry:    w.reg,
		MaxSteps:          in.maxSteps,
		ContextWindow:     in.entry.ContextWindow,
		RecentKeep:        w.cfg.Agent.RecentKeep,
		CompactionBudgets: compactionBudgets(w.cfg),
		CompactRatio:      w.cfg.Agent.CompactRatio,
		ContextEditing:    w.cfg.Agent.ContextEditing,
		Temperature:       w.cfg.Agent.Temperature,
		ArchiveDir:        w.roots.ArchiveDir(),
		SysPrompt:         "",
		Gate:              w.gate,
		KeepPolicy:        w.keep,
		SubagentModel:     in.sub.taskModel,
		SubagentEffort:    in.sub.taskEffort,
		InheritedEffort:   in.sub.inheritedFor,
		ResolveProvider:   in.sub.resolveProvider,
		HooksForRole:      func(role string) agent.ToolHooks { return w.hooks.ForRole(role) },
	}).
		WithTranscripts(in.store, in.root, in.modelName, in.entry.Effort).
		WithTranscriptIdentityResolver(in.sub.identity).
		WithMaxSubagentDepth(in.sub.maxDepth).
		WithDeliveryProfile(in.delivery).
		WithAblation(in.opts.Ablation).
		WithWorkspaceLease(in.session.lease).
		WithScheduler(in.sub.scheduler).
		WithProfileLookup(in.sub.profileLookup).
		WithProfileConfigResolvers(in.sub.profileModel, in.sub.profileEffort).
		WithBashSandboxEnforced(in.bashEnforced)
}

// skillRunOptions is the one place skill sub-agent options are built, so the
// read-only and writer-capable runners cannot drift on compaction or language.
func (w roleWiring) skillRunOptions(in delegationInputs) func(context.Context, int, *provider.Pricing, int, int) agent.Options {
	return func(sctx context.Context, steps int, price *provider.Pricing, ctxWin, childDepth int) agent.Options {
		return agent.Options{
			MaxSteps:          steps,
			Temperature:       w.cfg.Agent.Temperature,
			Pricing:           price,
			UsageSource:       event.UsageSourceSubagent,
			Gate:              w.gate,
			ContextWindow:     ctxWin,
			RecentKeep:        w.cfg.Agent.RecentKeep,
			CompactionBudgets: compactionBudgets(w.cfg),
			CompactRatio:      w.cfg.Agent.CompactRatio,
			ContextEditing:    w.cfg.Agent.ContextEditing,
			ArchiveDir:        w.roots.ArchiveDir(),
			KeepPolicy:        w.keep,
			ResponseLanguage:  langpref.ResponseLanguageFromContext(sctx),
			ReasoningLanguage: langpref.ReasoningLanguageFromContext(sctx),
			SubagentDepth:     childDepth,
			MaxSubagentDepth:  in.sub.maxDepth,
			DeliveryProfile:   in.delivery,
			Ablation:          in.opts.Ablation,
			WorkspaceLease:    in.session.lease,
			Hooks:             w.hooks.ForRole(skillSubagentRole(sctx)),
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
