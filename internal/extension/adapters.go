package extension

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/command"
	"reasonix/internal/hook"
	"reasonix/internal/plugin"
	"reasonix/internal/pluginpkg"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

// The adapters wrap existing discovery in the Contributor interface. They do
// not reimplement any winner rules: each source package keeps resolving its
// own intra-source clashes exactly as today (skill tier priority, command
// later-root-wins, hook accumulation), and the kernel resolves only what
// crosses contributor boundaries.

// BuiltinToolsContributor contributes the compile-time built-in tools as
// KindTool at the builtin tier. The built-in set is process-global, so the
// contributor is a pure function of whatever registered itself via init().
func BuiltinToolsContributor() Contributor {
	return ContributorFunc{
		ContributorName: "builtin-tools",
		Fn: func(context.Context) ([]Contribution, error) {
			tools := tool.Builtins()
			out := make([]Contribution, 0, len(tools))
			for _, t := range tools {
				out = append(out, Contribution{
					Kind:    KindTool,
					ID:      t.Name(),
					Source:  ContributionSource{Scope: ScopeBuiltin, Origin: "builtin"},
					Payload: t,
				})
			}
			return out, nil
		},
	}
}

// skillScope maps a discovered skill onto a kernel scope. Plugin ownership
// wins over the discovery scope because a plugin's skill must shadow and
// conflict as part of its package, not as the root it happened to load from.
// Custom roots have no kernel tier of their own; they map to global (the
// store's own project > custom > global ordering already ran inside List, so
// no cross-tier information is lost).
func skillScope(sk skill.Skill) (Scope, string) {
	if sk.Plugin != "" {
		return ScopePlugin, "plugin"
	}
	switch sk.Scope {
	case skill.ScopeProject:
		return ScopeProject, "project"
	case skill.ScopeBuiltin:
		return ScopeBuiltin, "builtin"
	case skill.ScopeCustom:
		return ScopeGlobal, "custom"
	default:
		return ScopeGlobal, "global"
	}
}

// SkillContribution maps one discovered skill to its kernel contribution. The
// canonical ID is the user-facing slash name, so a plugin skill ("pkg:name")
// and a project skill ("name") occupy distinct IDs exactly as they do for the
// user. Boot uses this to contribute the skill list its build already
// assembled, so scope attribution lives in exactly one place.
func SkillContribution(sk skill.Skill) Contribution {
	scope, origin := skillScope(sk)
	return Contribution{
		Kind: KindSkill,
		ID:   sk.SlashName(),
		Source: ContributionSource{
			PluginID: sk.Plugin,
			Scope:    scope,
			Origin:   origin,
			Path:     sk.Path,
		},
		Payload: sk,
	}
}

// SkillsContributor contributes a store's model-visible skills as KindSkill.
// The canonical ID is the user-facing slash name, so a plugin skill
// ("pkg:name") and a project skill ("name") occupy distinct IDs exactly as
// they do for the user. Store.List already dedupes bare names by tier, so
// what reaches the kernel is the effective set.
func SkillsContributor(store *skill.Store) Contributor {
	return ContributorFunc{
		ContributorName: "skills",
		Fn: func(context.Context) ([]Contribution, error) {
			if store == nil {
				return nil, nil
			}
			skills := store.List()
			out := make([]Contribution, 0, len(skills))
			for _, sk := range skills {
				out = append(out, SkillContribution(sk))
			}
			return out, nil
		},
	}
}

// CommandContribution maps one resolved command to its kernel contribution.
// Plugin commands map to the plugin tier. Non-plugin commands map to the
// project tier: command.LoadRoots has already collapsed user-vs-project
// ordering into a single winner, so the surviving entry represents the
// strongest applicable scope.
func CommandContribution(c command.Command) Contribution {
	source := ContributionSource{
		PluginID: c.Plugin,
		Scope:    ScopeProject,
		Origin:   "command_root",
		Path:     c.Source,
	}
	if c.Plugin != "" {
		source.Scope = ScopePlugin
		source.Origin = "plugin"
	}
	return Contribution{
		Kind:    KindCommand,
		ID:      c.Name,
		Source:  source,
		Payload: c,
	}
}

// CommandsContributor contributes the commands resolved by command.LoadRoots
// as KindCommand. LoadRoots' later-root-wins and plugin namespacing run
// first; the kernel sees the resolved set. A load error aborts the
// contribution — silently dropping a broken file is how a user's command
// disappears without a trace.
func CommandsContributor(roots ...command.Root) Contributor {
	return ContributorFunc{
		ContributorName: "commands",
		Fn: func(context.Context) ([]Contribution, error) {
			commands, err := command.LoadRoots(roots...)
			if err != nil {
				return nil, fmt.Errorf("extension: commands: %w", err)
			}
			out := make([]Contribution, 0, len(commands))
			for _, c := range commands {
				out = append(out, CommandContribution(c))
			}
			return out, nil
		},
	}
}

// HookContribution maps one resolved hook to its additive kernel
// contribution. seq is the hook's per-event index in load order, so the
// "event#n" ID matches the order hooks fire today (project, then plugin,
// then global within an event).
func HookContribution(h hook.ResolvedHook, seq int) Contribution {
	return Contribution{
		Kind: KindHook,
		ID:   fmt.Sprintf("%s#%d", h.Event, seq),
		Source: ContributionSource{
			Scope:  hookScope(h.Scope),
			Origin: h.Source,
			Path:   h.Source,
		},
		Payload: h,
	}
}

// HooksContributor contributes every resolved hook as additive KindHook
// entries. IDs are "event#n" with n counted per event in load order, matching
// the order the hooks fire today (project, then plugin, then global within an
// event). Hooks never shadow, so all of them survive resolution.
func HooksContributor(opts hook.LoadOptions) Contributor {
	return ContributorFunc{
		ContributorName: "hooks",
		Fn: func(context.Context) ([]Contribution, error) {
			hooks := hook.Load(opts)
			out := make([]Contribution, 0, len(hooks))
			perEvent := map[hook.Event]int{}
			for _, h := range hooks {
				n := perEvent[h.Event]
				perEvent[h.Event]++
				out = append(out, HookContribution(h, n))
			}
			return out, nil
		},
	}
}

// hookScope maps the settings-file scope of a hook. Plugin hooks carry their
// package's tier; the plugin identity itself stays in the payload because
// hook.ResolvedHook does not expose it separately.
func hookScope(s hook.Scope) Scope {
	switch s {
	case hook.ScopeProject:
		return ScopeProject
	case hook.ScopePlugin:
		return ScopePlugin
	default:
		return ScopeGlobal
	}
}

// MCPServerContribution maps one MCP server spec to its kernel contribution,
// keyed by server name — the same identity that namespaces the server's tools
// as mcp__<server>__<tool>.
func MCPServerContribution(spec plugin.Spec) Contribution {
	scope, origin := mcpScope(spec)
	return Contribution{
		Kind: KindMCPServer,
		ID:   spec.Name,
		Source: ContributionSource{
			PluginID: spec.Package,
			Scope:    scope,
			Origin:   origin,
		},
		Payload: spec,
	}
}

// MCPServersContributor contributes MCP server specs as KindMCPServer keyed
// by server name — the same identity that namespaces the server's tools as
// mcp__<server>__<tool>. Tier mapping follows the spec's provenance: plugin
// packages at the plugin tier, project/workspace config at the project tier,
// everything else (user config, host sessions) at the global tier.
func MCPServersContributor(specs ...plugin.Spec) Contributor {
	return ContributorFunc{
		ContributorName: "mcp-servers",
		Fn: func(context.Context) ([]Contribution, error) {
			out := make([]Contribution, 0, len(specs))
			for _, spec := range specs {
				out = append(out, MCPServerContribution(spec))
			}
			return out, nil
		},
	}
}

// mcpScope derives the kernel tier from a spec's provenance. Package is the
// strongest signal — it means an installed plugin brought the server along;
// ConfigSource distinguishes project/workspace config from user-level config
// (see internal/config.MCPConfigSource for the value set).
func mcpScope(spec plugin.Spec) (Scope, string) {
	configSource := strings.TrimSpace(spec.ConfigSource)
	if spec.Package != "" || configSource == "plugin_package" {
		if configSource == "" {
			configSource = "plugin_package"
		}
		return ScopePlugin, configSource
	}
	if strings.HasPrefix(configSource, "project") || configSource == "workspace_config" {
		return ScopeProject, configSource
	}
	if configSource == "" {
		configSource = "user_config"
	}
	return ScopeGlobal, configSource
}

// PromptContribution maps one plugin package prompt template to its kernel
// contribution as KindPrompt, namespaced "plugin:<plugin>:<name>" — the same
// namespacing the catalog documents for themes — so prompts from different
// packages can never collide. This adapter lives in extension rather than
// pluginpkg for the same reason it may import pluginpkg at all: pluginpkg
// cannot import extension (extension -> hook -> pluginpkg would become an
// import cycle), while extension importing pluginpkg is acyclic.
func PromptContribution(pluginID string, ref pluginpkg.PromptRef) Contribution {
	return Contribution{
		Kind: KindPrompt,
		ID:   "plugin:" + pluginID + ":" + ref.Name,
		Source: ContributionSource{
			PluginID: pluginID,
			Scope:    ScopePlugin,
			Origin:   "plugin",
			Path:     ref.Path,
		},
		Payload: ref,
	}
}

// ThemeContribution maps one plugin package theme file to its kernel
// contribution as KindTheme with the stable ID "plugin:<plugin>:<theme>"
// named in the catalog contract.
func ThemeContribution(pluginID string, ref pluginpkg.ThemeRef) Contribution {
	return Contribution{
		Kind: KindTheme,
		ID:   "plugin:" + pluginID + ":" + ref.Name,
		Source: ContributionSource{
			PluginID: pluginID,
			Scope:    ScopePlugin,
			Origin:   "plugin",
			Path:     ref.Path,
		},
		Payload: ref,
	}
}

// PluginResourcesContributor contributes one parsed plugin package's prompt
// templates (KindPrompt) and theme files (KindTheme). Runtime, interceptor,
// and strategy wiring is a later stage; this covers the manifest's static
// resource contributions.
func PluginResourcesContributor(pkg pluginpkg.Package) Contributor {
	return ContributorFunc{
		ContributorName: "plugin-resources-" + pkg.Manifest.Name,
		Fn: func(context.Context) ([]Contribution, error) {
			inv := pkg.Inventory()
			out := make([]Contribution, 0, len(inv.Prompts)+len(inv.Themes))
			for _, ref := range inv.Prompts {
				out = append(out, PromptContribution(pkg.Manifest.Name, ref))
			}
			for _, ref := range inv.Themes {
				out = append(out, ThemeContribution(pkg.Manifest.Name, ref))
			}
			return out, nil
		},
	}
}

// ProviderContribution maps one provider descriptor to its kernel
// contribution, keyed by ref ("provider/model") at the builtin tier.
func ProviderContribution(desc provider.Descriptor) Contribution {
	return Contribution{
		Kind:    KindProvider,
		ID:      desc.Ref,
		Source:  ContributionSource{Scope: ScopeBuiltin, Origin: "provider_catalog"},
		Payload: desc,
	}
}

// ProvidersContributor contributes provider descriptors as KindProvider keyed
// by ref ("provider/model"). Descriptors come from the built-in provider
// catalog, so they sit at the builtin tier until a stage-3 source contributes
// user-defined providers above them.
func ProvidersContributor(descs ...provider.Descriptor) Contributor {
	return ContributorFunc{
		ContributorName: "providers",
		Fn: func(context.Context) ([]Contribution, error) {
			out := make([]Contribution, 0, len(descs))
			for _, desc := range descs {
				out = append(out, ProviderContribution(desc))
			}
			return out, nil
		},
	}
}
