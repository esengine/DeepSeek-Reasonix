package boot

import (
	"context"
	"os"

	"reasonix/internal/ablation"
	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
	"reasonix/internal/workspacelease"
)

func newSubagentSkillOptionsFactory(
	cfg config.AgentConfig,
	quoteCtx *event.QuoteContext,
	gate agent.Gate,
	keepPolicy agent.KeepPolicy,
	maxDepth int,
	ablationSet ablation.Set,
	lease *workspacelease.Owner,
	writeRoots *sandbox.WritableRootSet,
) func(context.Context, int, *provider.Pricing, int, int) agent.Options {
	home, stateRoot := userHomeDir(), config.MemoryUserDir()
	return func(ctx context.Context, steps int, price *provider.Pricing, ctxWin, childDepth int) agent.Options {
		return agent.Options{
			MaxSteps: steps, Temperature: cfg.Temperature, Pricing: price, QuoteContext: quoteCtx, UsageSource: event.UsageSourceSubagent,
			Gate: gate, ContextWindow: ctxWin, RecentKeep: cfg.RecentKeep,
			SoftCompactRatio: cfg.SoftCompactRatio, ToolResultSnipRatio: cfg.ToolResultSnipRatio,
			CompactRatio: cfg.CompactRatio, CompactForceRatio: cfg.CompactForceRatio, ContextEditing: cfg.ContextEditing,
			ArchiveDir: config.ArchiveDir(), KeepPolicy: keepPolicy,
			ResponseLanguage: agent.ResponseLanguageFromContext(ctx), ReasoningLanguage: agent.ReasoningLanguageFromContext(ctx),
			SubagentDepth: childDepth, MaxSubagentDepth: maxDepth,
			Ablation: ablationSet, WorkspaceLease: lease, WriteRoots: writeRoots,
			DisableWriteAccessExpand: true, HomeDir: home, StateRoot: stateRoot,
		}
	}
}

func skillSubagentRegistry(
	sk skill.Skill,
	parent *tool.Registry,
	childDepth, maxDepth int,
	runtime *agent.MCPCapabilityRuntime,
	writeRoots *sandbox.WritableRootSet,
) (*tool.Registry, *sandbox.WritableRootSet) {
	if sk.ReadOnly {
		return agent.ReadOnlySubagentToolRegistryForDepthWithRuntime(parent, sk.AllowedTools, childDepth, maxDepth, runtime), nil
	}
	reg := agent.SubagentToolRegistryForDepthWithRuntime(parent, sk.AllowedTools, childDepth, maxDepth, runtime)
	return agent.BindChildWriteRoots(reg, writeRoots, agent.WritePathSet{})
}

func projectWriteAccessPersister(root string) control.PersistWriteAccessFunc {
	return func(dirs []string, permRule string) error {
		return config.PersistProjectWriteAccess(rememberPermissionConfigPath(root), dirs, permRule)
	}
}

func userHomeDir() string {
	home, _ := os.UserHomeDir()
	return home
}
