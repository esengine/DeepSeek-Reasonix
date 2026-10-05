package extension

import (
	"fmt"
	"strings"

	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/command"
	"reasonix/internal/ext/hook"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/ext/skill"
)

// Each function here maps one already-discovered record onto one Contribution.
// Discovery and a source's own intra-source winner rules (skill tier priority,
// command later-root-wins, hook accumulation) stay in the source package and
// have run before a record arrives; the kernel resolves only what crosses
// contributor boundaries.

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

// mcpScope derives the kernel tier from a spec's provenance. Package is the
// strongest signal — it means an installed plugin brought the server along;
// ConfigSource distinguishes project/workspace config from user-level config
// (see internal/contract/config.MCPConfigSource for the value set).
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
