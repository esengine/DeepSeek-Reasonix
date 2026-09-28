package boot

import (
	"context"
	"fmt"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/platform/lsp"
)

// registerMCPTools puts every enabled server into the tool catalog and returns
// the configured specs it registered, with the names of those whose tools were
// known at registration. Host-session servers take a short readiness probe;
// configured ones stay process-idle until their first call.
func registerMCPTools(ctx context.Context, host *plugin.Host, reg *tool.Registry, plan mcpSpecPlan, sink event.Sink) ([]plugin.Spec, map[string]bool) {
	for _, s := range plan.extra {
		registerHostSessionServer(ctx, host, reg, s, sink)
	}
	known := map[string]bool{}
	for _, s := range plan.configured {
		if registerConfiguredServer(ctx, host, reg, s, plan.alwaysLoad[s.Name]) {
			known[s.Name] = true
		}
	}
	return plan.configured, known
}

// registerHostSessionServer connects a server the host session named for this
// controller, so recovery and session-scoped servers are deterministic. A
// failure still leaves a catalog entry for /mcp to diagnose.
func registerHostSessionServer(ctx context.Context, host *plugin.Host, reg *tool.Registry, s plugin.Spec, sink event.Sink) {
	if host.HasClient(s.Name) {
		if tools, err := host.ToolsFor(ctx, s.Name); err == nil {
			addTools(reg, tools)
			return
		}
	}
	addCtx, addCancel := context.WithTimeout(ctx, 5*time.Second)
	tools, err := host.EnsureConnectedWithLifecycle(ctx, addCtx, s, 0)
	addCancel()
	if err == nil {
		addTools(reg, tools)
		return
	}
	if plugin.IsServerAlreadyConnected(err) {
		if tools, err2 := host.ToolsFor(ctx, s.Name); err2 == nil {
			addTools(reg, tools)
			return
		}
	}
	cs, _ := plugin.LoadCachedSchemaForSpec(s)
	addTools(reg, plugin.LazyToolset(s, cs, host, reg, ctx, false))
	report(sink, event.Event{Level: event.LevelWarn,
		Text: "An MCP server failed to start.", Detail: fmt.Sprintf("mcp %s: %v", s.Name, err)})
}

// registerConfiguredServer registers placeholders from the cached schema. It
// starts a process at once for catalog discovery when no usable schema is
// cached, and for an authorized always-loaded server, whose tools the model
// may call directly on its first turn; any other server connects on first use.
// It reports whether the server's tools were known when it returned.
func registerConfiguredServer(ctx context.Context, host *plugin.Host, reg *tool.Registry, s plugin.Spec, alwaysLoad bool) bool {
	if host.HasClient(s.Name) {
		if tools, err := host.ToolsFor(ctx, s.Name); err == nil {
			addTools(reg, tools)
			return true
		}
	}
	cs, _ := plugin.LoadCachedSchemaForSpec(s)
	known := cs != nil && len(cs.Tools) > 0
	preconnect := alwaysLoad && plugin.ResolveStoredAuthorization(ctx, s).ServerAuthorized()
	addTools(reg, plugin.LazyToolset(s, cs, host, reg, ctx, !known || preconnect))
	return known
}

func withoutSpecs(specs, drop []plugin.Spec) []plugin.Spec {
	if len(drop) == 0 {
		return specs
	}
	names := make(map[string]bool, len(drop))
	for _, s := range drop {
		names[s.Name] = true
	}
	kept := specs[:0]
	for _, s := range specs {
		if !names[s.Name] {
			kept = append(kept, s)
		}
	}
	return kept
}

func addTools(reg *tool.Registry, tools []tool.Tool) {
	for _, t := range tools {
		reg.Add(t)
	}
}

// registerLSP adds the LSP tools. Servers resolve on PATH and spawn on first
// query, so registering is cheap even with none installed.
func registerLSP(reg *tool.Registry, cfg *config.Config, root string) *lsp.Manager {
	if !cfg.LSP.Enabled {
		return nil
	}
	specs := LSPSpecs(cfg.LSP)
	for lang, spec := range specs {
		spec.Verify = cfg.ProjectProgramVerifier(config.ProjectProgramLSP, lang)
		specs[lang] = spec
	}
	mgr := lsp.NewManager(root, specs)
	for _, t := range lsp.Tools(mgr) {
		if t != nil {
			reg.Add(t)
		}
	}
	return mgr
}
