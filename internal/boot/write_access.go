package boot

import (
	"context"
	"fmt"
	"io"
	"os"

	"reasonix/internal/ablation"
	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/hook"
	"reasonix/internal/lsp"
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
	hookRunner *hook.Runner,
	imageRoutes ...childImageRouting,
) func(context.Context, int, *provider.Pricing, int, int) agent.Options {
	home, stateRoot := userHomeDir(), config.MemoryUserDir()
	return func(ctx context.Context, steps int, price *provider.Pricing, ctxWin, childDepth int) agent.Options {
		opts := agent.Options{
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
		if len(imageRoutes) > 0 {
			opts.ImageRequestResolver = imageRoutes[0].controller()
			opts.ImageInput = imageRoutes[0].config
		}
		callID, _, _, _ := agent.CallContext(ctx)
		opts.Hooks = hookRunner.ForSession("subagent:" + callID)
		return opts
	}
}

func newBootHookRunner(resolved []hook.ResolvedHook, root string, shell sandbox.Shell, sink event.Sink) *hook.Runner {
	if held, pending := hook.PendingProjectHooks(hook.LoadOptions{ProjectRoot: root}); pending {
		sink.Emit(projectHooksHeldEvent(held))
	}
	return newHookRunner(resolved, root, shell,
		func(msg string) { sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: msg}) })
}

// NewCommandHookRunner resolves hooks for a command that drives an agent
// without Build, from the sources load names; hooks run in load.ProjectRoot.
// Resolving shellCfg may execute its path, so it must come from a source the
// command trusts as much as the hooks themselves. Warnings go to warn.
func NewCommandHookRunner(shellCfg config.ShellConfig, load hook.LoadOptions, warn io.Writer) *hook.Runner {
	shell := sandbox.ResolveShell(shellCfg.Prefer, shellCfg.Path, warn)
	return newHookRunner(hook.Load(load), load.ProjectRoot, shell,
		func(msg string) { _, _ = fmt.Fprintln(warn, msg) })
}

func newHookRunner(resolved []hook.ResolvedHook, root string, shell sandbox.Shell, notify func(string)) *hook.Runner {
	runtime := hook.RuntimeOptions{}
	if shell.Kind == sandbox.ShellBash {
		runtime.BashPath = shell.Path
	}
	return hook.NewRunner(resolved, root, hook.NewDefaultSpawner(runtime), notify)
}

func reviewSubagentSkillOptions(
	ctx context.Context,
	profile, task string,
	steps int,
	price *provider.Pricing,
	ctxWin, childDepth int,
	factory func(context.Context, int, *provider.Pricing, int, int) agent.Options,
) (string, agent.Options) {
	reviewTokens := 0
	if reviewTask, tokens, ok := agent.PrepareReviewSubagentContext(ctx, profile, task); ok {
		task, reviewTokens = reviewTask, tokens
	}
	opts := factory(ctx, steps, price, ctxWin, childDepth)
	if reviewTokens > 0 {
		opts.MaxOutputTokens = reviewTokens
	}
	return task, opts
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
		return config.PersistWorkspaceWriteAccess(config.ReasonixHomeDir(), root, dirs, permRule)
	}
}

func userHomeDir() string {
	home, _ := os.UserHomeDir()
	return home
}

// projectHooksHeldEvent says which project hooks did not load and how to let them.
func projectHooksHeldEvent(p config.ProjectProgram) event.Event {
	return event.Event{
		Kind:   event.Notice,
		Level:  event.LevelWarn,
		Text:   "This project's hooks are off until you approve them.",
		Detail: fmt.Sprintf("%s declares: %s\nRun `reasonix trust` in this workspace to review and approve them; any later change needs approval again.", p.Name, p.Detail),
	}
}

// verifiedLSPSpecs checks a workspace-declared server against its approval
// before every start, so a file it names that changed mid-session is refused.
func verifiedLSPSpecs(cfg *config.Config) map[string]lsp.ServerSpec {
	specs := LSPSpecs(cfg.LSP)
	for lang, spec := range specs {
		spec.Verify = cfg.ProjectProgramVerifier(config.ProjectProgramLSP, lang)
		specs[lang] = spec
	}
	return specs
}
