package boot

import (
	"context"
	"fmt"
	"io"
	"strings"

	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/command"
	"reasonix/internal/ext/hook"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/ext/pluginspec"
	"reasonix/internal/ext/skill"
	"reasonix/internal/runtime/capability"
	"reasonix/internal/runtime/usecap"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/history"
	"reasonix/internal/state/memory"
	"reasonix/internal/tools/productdocs"
	"reasonix/internal/tools/sessiontool"
)

// loadHooks resolves the hooks, reusing the previous build's discovery when
// its plan allows, and binds the runner to the resolved shell.
func loadHooks(opts Options, roots config.Roots, root string, shell sandbox.Shell, sink event.Sink) ([]hook.ResolvedHook, *hook.Runner) {
	var resolved []hook.ResolvedHook
	if opts.ReuseAssembly != nil && shouldReuseDiscovery(opts.PreviousPlan) {
		resolved = opts.ReuseAssembly.Hooks
	} else {
		resolved = hook.Load(hook.LoadOptions{ProjectRoot: root, ReasonixHomeDir: roots.Home()})
		if held, pending := hook.PendingProjectHooks(hook.LoadOptions{ProjectRoot: root, ReasonixHomeDir: roots.Home()}); pending {
			sink.Emit(projectHooksHeldEvent(held))
		}
	}
	return resolved, hook.NewRunner(resolved, root, hook.NewDefaultSpawner(hookRuntime(shell)), func(n hook.Notice) { sink.Emit(hookNoticeEvent(n)) })
}

// NewUserHookRunner resolves only the hooks the user configured under the
// Reasonix home, for a command that drives an agent over a checkout it must not
// take commands from. Hook notices go to warn.
func NewUserHookRunner(cfg *config.Config, root string, warn io.Writer) *hook.Runner {
	return newUserHookRunner(cfg, root, warn, sandbox.ResolveShell, hook.NewDefaultSpawner)
}

// newUserHookRunner resolves the hook interpreter from the user config alone:
// resolving it probes the configured path, so a checkout's [tools.shell] would
// otherwise execute on the reviewer's machine before any hook matched. Hooks
// start in the checkout but never resolve a bare command name from it.
func newUserHookRunner(cfg *config.Config, root string, warn io.Writer, resolve func(prefer, path string, warn io.Writer) sandbox.Shell, spawn func(hook.RuntimeOptions) hook.Spawner) *hook.Runner {
	user := cfg.Roots().UserShell()
	shell := resolve(user.Prefer, user.Path, warn)
	resolved := hook.Load(hook.LoadOptions{ProjectRoot: root, ReasonixHomeDir: cfg.Roots().Home(), Scopes: hook.UserScopes})
	return hook.NewRunner(resolved, root, hook.WithoutCwdExeSearch(spawn(hookRuntime(shell))), func(n hook.Notice) {
		_, _ = fmt.Fprintln(warn, strings.TrimSpace(n.Text+"\n"+n.Detail))
	})
}

func hookRuntime(shell sandbox.Shell) hook.RuntimeOptions {
	if shell.Kind == sandbox.ShellBash {
		return hook.RuntimeOptions{BashPath: shell.Path}
	}
	return hook.RuntimeOptions{}
}

// registerSessionTools adds the product-docs, session and memory tools every
// role carries. The retrieval ablation drops only the BM25-backed history and
// recall, so a lost solve is attributable to retrieval, not a missing reader.
func registerSessionTools(reg *tool.Registry, set ablation.Set, roots config.Roots, sessionDir string, store memory.Store) {
	reg.Add(productdocs.NewTool())
	retrievalOff := set.Off(ablation.Retrieval)
	if !retrievalOff {
		reg.Add(history.NewIndexedTool(history.Options{SessionDir: sessionDir, GlobalSessionDir: roots.SessionDir(), ArchiveDir: roots.ArchiveDir()}))
	}
	reg.Add(sessiontool.NewListSessionsTool(sessionDir))
	reg.Add(sessiontool.NewReadSessionTool(sessionDir))
	if !retrievalOff {
		reg.Add(memory.NewRecallTool(store))
	}
	reg.Add(memory.NewRememberTool(store))
	reg.Add(memory.NewForgetTool(store))
	addTurnExitTools(reg)
}

func loadCommands(opts Options, root string) []command.Command {
	if opts.ReuseAssembly != nil && shouldReuseDiscovery(opts.PreviousPlan) {
		return opts.ReuseAssembly.Commands
	}
	cmds, _ := command.LoadRoots(config.CommandRootsForRoot(root)...)
	return cmds
}

// skillRunners are the entry points a skill runs through as a sub-agent.
type skillRunners struct {
	readOnly skill.SubagentRunner
	run      skill.SubagentRunner
	profile  skill.ProfileResolver
}

// registerSkillTools adds the skill tools and the slash-command tool. Skill
// tools and their slash entries share one switch, since implicit invocation
// decides whether the model sees skills at all; the slash tool goes last so
// it carries every entry.
func registerSkillTools(reg *tool.Registry, set ablation.Set, store *skill.Store, implicit bool, runners skillRunners, cmds []command.Command) {
	var entries []command.SlashEntry
	if implicit {
		reg.Add(skill.NewReadOnlySkillTool(store, gateSubagentArm(set, runners.readOnly), runners.profile))
		reg.Add(skill.NewRunSkillTool(store, gateSubagentArm(set, runners.run), runners.profile))
		reg.Add(skill.NewReadSkillTool(store))
		reg.Add(skill.NewInstallSkillTool(store, nil))
		addTools(reg, builtinSubagentTools(set, store, runners.run, runners.profile))
		for _, sk := range store.SlashList() {
			entries = append(entries, command.SlashEntry{
				Name:        sk.SlashName(),
				Description: sk.Description,
				Render: func(args []string) string {
					return skill.RenderInvocation(store.Prepare(sk), strings.Join(args, " "))
				},
			})
		}
	}
	for _, cmd := range cmds {
		if cmd.Hidden {
			continue
		}
		entries = append(entries, command.SlashEntry{
			Name:        cmd.Name,
			Description: cmd.Description,
			ArgHint:     cmd.ArgHint,
			Render:      func(args []string) string { return cmd.Render(args) },
		})
	}
	reg.Add(command.NewSlashCommandTool(entries))
}

// capabilitySurface is the session-shared MCP runtime behind use_capability:
// one host and spec set, with a frontend per agent so each keeps its own
// ledger and audit while reusing the processes.
type capabilitySurface struct {
	specs   []plugin.Spec
	runtime *usecap.MCPCapabilityRuntime
	ledger  *capability.Ledger
	audit   *capability.Audit
	proxy   *usecap.UseCapabilityTool
	catalog func() capability.Catalog
}

func newCapabilitySurface(ctx context.Context, cfg *config.Config, root string, specOptions pluginspec.Options, host *plugin.Host, reg *tool.Registry, skills *skill.Store, profile capability.Profile, enabled map[string]bool) *capabilitySurface {
	c := &capabilitySurface{specs: pluginspec.ForRootWithOptions(cfg.Plugins, root, specOptions)}
	cachedTools, cacheKeyOK := capability.LoadCachedToolsForSpecs(c.specs)
	skills.ConfigureToolBindings(func(sk skill.Skill) []tool.MCPBinding {
		return skillMCPBindings(sk, reg, c.specs, cachedTools, cacheKeyOK)
	})
	// AllContractEntries: use_capability also dispatches tools the provider never sees.
	c.catalog = func() capability.Catalog {
		conn := map[string]bool{}
		failedNow := map[string]string{}
		if host != nil {
			for _, n := range host.ServerNames() {
				conn[n] = true
			}
			for _, failure := range host.Failures() {
				failedNow[failure.Name] = failure.Error
			}
		}
		catOpts := capability.CatalogOptions{
			Tools:       reg.AllContractEntries(),
			Skills:      skills.List(),
			Plugins:     cfg.Plugins,
			Profile:     profile,
			Connected:   conn,
			Failed:      failedNow,
			CachedTools: cachedTools,
			CacheKeyOK:  cacheKeyOK,
		}
		if c.runtime != nil {
			catOpts.Plugins, catOpts.CachedTools, catOpts.CacheKeyOK, catOpts.Disabled, catOpts.ProxyTools = c.runtime.CapabilityCatalogState()
		}
		return capability.BuildCatalog(catOpts)
	}
	// WithoutCancel: ctx is often one request, and MCP children outlive it.
	c.runtime = usecap.NewMCPCapabilityRuntime(context.WithoutCancel(ctx), host, c.specs, reg, c.catalog)
	c.runtime.ConfigureServers(cfg.Plugins, c.specs, enabled)
	c.ledger = capability.NewLedger()
	c.audit = &capability.Audit{}
	c.proxy = c.runtime.NewFrontend(c.ledger, c.audit)
	return c
}
