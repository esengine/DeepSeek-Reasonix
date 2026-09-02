package control

import (
	"context"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/capability"
	"reasonix/internal/config"
	"reasonix/internal/plugin"
)

func (c *Controller) withCapabilityRoute(ctx context.Context, composed, routeInput string) string {
	if c == nil {
		return composed
	}
	routeInput = strings.TrimSpace(agent.StripTransientUserBlocks(routeInput))
	if routeInput == "" {
		routeInput = strings.TrimSpace(agent.StripTransientUserBlocks(composed))
	}
	if routeInput == "" {
		return composed
	}
	decision := c.routeCapabilities(ctx, routeInput)
	// Pass structured decision to the agent via ledger — never re-parse the prompt.
	if c.executor != nil {
		c.executor.SeedCapabilityRoute(decision)
	}
	// Dual-model Planner also consumes the route through the user turn; seed
	// its ledger when the runner exposes a planner agent.
	if c.runner != nil {
		if coord, ok := c.runner.(interface{ PlannerAgent() *agent.Agent }); ok {
			if p := coord.PlannerAgent(); p != nil {
				p.SeedCapabilityRoute(decision)
			}
		}
	}
	block := capability.RenderTransientBlock(decision)
	if block == "" {
		return composed
	}
	return block + "\n\n" + composed
}

func (c *Controller) routeCapabilities(ctx context.Context, routeInput string) capability.RouteDecision {
	if ctx == nil {
		ctx = context.Background()
	}
	tools := c.ToolContractEntries()
	// Deterministic routing is first. The semantic router runs only when that
	// catalog match is itself ambiguous — never as a per-turn classification.
	var proxyTools map[string][]plugin.CachedTool
	if c.proxyToolsFn != nil {
		proxyTools = c.proxyToolsFn()
	}
	if proxyTools == nil {
		if reg := c.mcp.registry(); reg != nil {
			if t, ok := reg.Get("use_capability"); ok {
				if p, ok := t.(interface {
					ConnectedProxyTools() map[string][]plugin.CachedTool
				}); ok {
					proxyTools = p.ConnectedProxyTools()
				}
			}
		}
	}
	opts := capability.CatalogOptions{
		Tools:  tools,
		Skills: c.Skills(),
	}
	if c.capabilityRuntime != nil {
		opts.Plugins, opts.CachedTools, opts.CacheKeyOK, opts.Disabled, proxyTools = c.capabilityRuntime.CapabilityCatalogState()
	} else if c.pluginCfg != nil {
		opts.Plugins = c.pluginCfg
		opts.CachedTools = c.capCachedTools
		opts.CacheKeyOK = c.capCacheKeyOK
	}
	// Cached MCP tool schemas (loaded once in WireCapabilityRouting) let
	// auto_start=false servers contribute concrete mcp-tool candidates to
	// deterministic and semantic routing before any connection exists.
	opts.ProxyTools = proxyTools
	if h := c.Host(); h != nil {
		opts.Connected = map[string]bool{}
		for _, n := range h.ServerNames() {
			opts.Connected[n] = true
		}
		opts.Failed = map[string]string{}
		for _, f := range h.Failures() {
			opts.Failed[f.Name] = f.Error
		}
	}
	catalog := capability.BuildCatalog(opts)
	decision := capability.Route(routeInput, catalog.Entries)
	if c.capabilityProxy {
		decision.CapabilityProxy = true
	}

	strong := false
	for _, cand := range decision.Candidates {
		if cand.Policy == capability.AutoUseRequire || cand.Policy == capability.AutoUsePrefer {
			strong = true
			break
		}
	}
	ambiguous := !strong && len(decision.Candidates) > 1
	if ambiguous && c.semanticRouter != nil {
		before := len(decision.Candidates)
		decision = c.semanticRouter.RouteSemantic(ctx, routeInput, catalog, decision)
		if c.capabilityProxy {
			decision.CapabilityProxy = true
		}
		if c.capabilityAudit != nil {
			c.capabilityAudit.RecordRoute(true, len(decision.Candidates) == before)
		}
	} else if c.capabilityAudit != nil {
		c.capabilityAudit.RecordRoute(false, false)
	}
	if c.capabilityAudit != nil {
		c.capabilityAudit.RecordDecision(decision)
	}
	return decision
}

// WireCapabilityRouting attaches hybrid routing helpers. Safe to call with nil
// semantic router (deterministic only). specs are the boot-converted plugin
// specs; their persisted schema caches are loaded once here so every routing
// turn can offer cached tools of not-yet-started servers.
func (c *Controller) WireCapabilityRouting(plugins []config.PluginEntry, specs []plugin.Spec, router *capability.SemanticRouter, audit *capability.Audit) {
	if c == nil {
		return
	}
	c.pluginCfg = append([]config.PluginEntry(nil), plugins...)
	c.capCachedTools, c.capCacheKeyOK = capability.LoadCachedToolsForSpecs(specs, c.mcpHostProfile())
	c.semanticRouter = router
	c.capabilityAudit = audit
}

// SetCapabilityProxyRouting directs unready MCP route candidates to
// use_capability instead of connect_tool_source. Used by closed-loop routes and
// dual-model Planner boots.
func (c *Controller) SetCapabilityProxyRouting(v bool) {
	if c == nil {
		return
	}
	c.capabilityProxy = v
}

// SetCapabilityProxyTools registers a getter for live tools observed through
// use_capability without entering the provider-visible registry.
func (c *Controller) SetCapabilityProxyTools(fn func() map[string][]plugin.CachedTool) {
	if c == nil {
		return
	}
	c.proxyToolsFn = fn
}
