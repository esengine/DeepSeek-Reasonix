package boot

import (
	"context"
	"fmt"
	"runtime"
	"strings"
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
		plugin.ApplyDisabledMCPPolicy(reg, s)
		registerHostSessionServer(ctx, host, reg, s, sink)
	}
	known := map[string]bool{}
	for _, s := range plan.configured {
		plugin.ApplyDisabledMCPPolicy(reg, s)
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
	if host.HasClientForSpec(s) {
		if tools, err := host.ToolsForSpec(ctx, s); err == nil {
			addServerTools(reg, host, s, tools)
			return
		}
	}
	addCtx, addCancel := context.WithTimeout(ctx, 5*time.Second)
	tools, err := host.EnsureConnectedWithLifecycle(ctx, addCtx, s, 0)
	addCancel()
	if err == nil {
		addServerTools(reg, host, s, tools)
		return
	}
	if plugin.IsServerAlreadyConnected(err) {
		if tools, err2 := host.ToolsForSpec(ctx, s); err2 == nil {
			addServerTools(reg, host, s, tools)
			return
		}
	}
	host.RecordFailure(s, err)
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
	if host.HasClientForSpec(s) {
		if tools, err := host.ToolsForSpec(ctx, s); err == nil {
			addServerTools(reg, host, s, tools)
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

// addServerTools registers a connected server's tools and the held set its
// client withheld, so a call to a withheld tool is refused with its cause.
func addServerTools(reg *tool.Registry, host *plugin.Host, s plugin.Spec, tools []tool.Tool) {
	addTools(reg, tools)
	plugin.ApplyHeldMCPPolicy(reg, host, s)
}

func addTools(reg *tool.Registry, tools []tool.Tool) {
	for _, t := range tools {
		reg.Add(t)
	}
}

// lspWithoutServer reports whether this host could only ever answer
// `language server "gopls" not found on PATH`, so the tools should stay out of
// the schema rather than be offered and then refuse on the first call.
//
// Android is that host, and for the same reason it has no browser tools: every
// built-in spec launches a language server off PATH (`gopls`, `rust-analyzer`,
// `typescript-language-server`, …), and a phone has none of them, no package
// manager to install one with, and no place a project's sources would be. A
// remote workspace is the case a phone actually has — and there the server runs
// on the machine the workspace is on, so this host's own PATH is not what
// answers.
//
// A host that names a command in `[lsp.servers]` is taken at its word, the way
// a named `[browser] executable` is: someone who says where their language
// server is has one, and the manager decides the rest at first query.
func lspWithoutServer(goos string, cfg config.LSPConfig) bool {
	if goos != "android" {
		return false
	}
	for _, spec := range LSPSpecs(cfg) {
		if strings.TrimSpace(spec.Command) != "" && !isBuiltinLSPCommand(spec.Command) {
			return false
		}
	}
	return true
}

// isBuiltinLSPCommand reports a command that is one of the shipped defaults
// rather than a host's own. The defaults are what a phone cannot have; a
// command someone wrote into their config is the one case this check yields to.
func isBuiltinLSPCommand(command string) bool {
	for _, spec := range lsp.DefaultSpecs() {
		if spec.Command == command {
			return true
		}
	}
	return false
}

// registerLSP adds the LSP tools. Servers resolve on PATH and spawn on first
// query, so registering is cheap even with none installed.
func registerLSP(reg *tool.Registry, cfg *config.Config, root string) *lsp.Manager {
	if !cfg.LSP.Enabled {
		return nil
	}
	if lspWithoutServer(runtime.GOOS, cfg.LSP) {
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
