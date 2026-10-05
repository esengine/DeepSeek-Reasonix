package boot

import (
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/ext/pluginspec"
)

// mcpSpecPlan is what a build resolves about MCP servers before any process is
// touched: the configured and host-session specs, the names config has
// enabled, and the configured servers whose tools load into the schema.
type mcpSpecPlan struct {
	configured []plugin.Spec
	extra      []plugin.Spec
	enabled    map[string]bool
	alwaysLoad map[string]bool
}

// resolveMCPSpecs resolves the enabled servers and normalizes the host-session
// extras.
func resolveMCPSpecs(opts Options, cfg *config.Config, root string, specOptions pluginspec.Options) mcpSpecPlan {
	autoStart := cfg.EnabledPlugins(root, config.DefaultActivationStore())
	enabled := make(map[string]bool, len(autoStart))
	alwaysLoad := map[string]bool{}
	for _, e := range autoStart {
		if name := strings.TrimSpace(e.Name); name != "" {
			enabled[name] = true
			if cfg.MCPAlwaysLoad(e) {
				alwaysLoad[name] = true
			}
		}
	}
	extra := pluginspec.ApplyDefaultStartupTimeout(
		pluginspec.ApplyDefaultCallTimeout(
			pluginspec.ApplyKnownOverrides(opts.ExtraPlugins, root),
			specOptions.DefaultCallTimeout,
		),
		specOptions.DefaultStartupTimeout,
	)
	for i := range extra {
		if strings.TrimSpace(extra[i].WorkspaceRoot) == "" {
			extra[i].WorkspaceRoot = root
		}
		if extra[i].LaunchManager == nil {
			extra[i].LaunchManager = specOptions.LaunchManager
		}
		if strings.TrimSpace(extra[i].ConfigSource) == "" {
			extra[i].ConfigSource = "host_session"
		}
		if !extra[i].RequireLaunchApproval {
			// Session-scoped MCP specs arrive through an explicit host/user action
			// (for example ACP session/new), so they follow installed-server
			// authorization without another per-tool or per-session prompt.
			extra[i].Authorized = true
		}
		pluginspec.ApplyIsolation(&extra[i], root, specOptions)
	}

	configured := withoutSpecs(pluginspec.ForRootWithOptions(autoStart, root, specOptions), extra)
	if opts.Stderr != nil {
		for i := range configured {
			configured[i].Stderr = opts.Stderr
		}
		for i := range extra {
			extra[i].Stderr = opts.Stderr
		}
	}
	return mcpSpecPlan{configured: configured, extra: extra, enabled: enabled, alwaysLoad: alwaysLoad}
}
