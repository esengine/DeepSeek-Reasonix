package control

import (
	"context"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/ext/skill"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/capability"
	"reasonix/internal/state/sessionstore"
)

type invokedSkillsKey struct{}

func withInvokedSkills(ctx context.Context, names []string) context.Context {
	if len(names) == 0 {
		return ctx
	}
	invoked := make(map[string]bool, len(names))
	for _, name := range names {
		invoked[strings.ToLower(name)] = true
	}
	return context.WithValue(ctx, invokedSkillsKey{}, invoked)
}

func invokedSkills(ctx context.Context) map[string]bool {
	invoked, _ := ctx.Value(invokedSkillsKey{}).(map[string]bool)
	return invoked
}

func (c *Controller) withCapabilityRoute(ctx context.Context, composed, routeInput string) string {
	if c == nil {
		return composed
	}
	routeInput = strings.TrimSpace(sessionstore.StripTransientUserBlocks(routeInput))
	// A resolved inline invocation already supplies the skill body. With no
	// typed task, routing on that body would invent a second skill request.
	if routeInput == "" && len(invokedSkills(ctx)) == 0 {
		routeInput = strings.TrimSpace(sessionstore.StripTransientUserBlocks(composed))
	}
	if routeInput == "" && len(invokedSkills(ctx)) == 0 {
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
	profile := capability.ProfileBalanced
	if c.AgentPreset() == "delivery" {
		profile = capability.ProfileDelivery
	}
	delivery := profile == capability.ProfileDelivery
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
		Tools:   tools,
		Skills:  c.skills.pathHits.Visible(skill.ModelInvocable(c.Skills())),
		Profile: profile,
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
	if invoked := invokedSkills(ctx); len(invoked) > 0 {
		entries := make([]capability.Entry, 0, len(catalog.Entries))
		for _, entry := range catalog.Entries {
			if entry.Kind != capability.KindSkill || !invoked[strings.ToLower(entry.Name)] {
				entries = append(entries, entry)
			}
		}
		catalog.Entries = entries
	}
	var decision capability.RouteDecision
	if delivery {
		decision = capability.RouteDelivery(routeInput, catalog.Entries)
	} else {
		decision = capability.Route(routeInput, catalog.Entries)
	}
	if c.capabilityProxy {
		decision.CapabilityProxy = true
	}

	// Deterministic routing comes first under every role setting; only Delivery
	// pays a model round-trip when it produced no strong candidate.
	if delivery && c.semanticRouter != nil {
		before := len(decision.Candidates)
		strong := false
		for _, cand := range decision.Candidates {
			if cand.Policy == capability.AutoUseRequire || cand.Policy == capability.AutoUsePrefer {
				strong = true
				break
			}
		}
		if !strong {
			decision = c.semanticRouter.RouteSemantic(ctx, routeInput, catalog, decision)
			if c.capabilityProxy {
				decision.CapabilityProxy = true
			}
			if c.capabilityAudit != nil {
				fallback := len(decision.Candidates) == before
				c.capabilityAudit.RecordRoute(true, fallback)
			}
		} else if c.capabilityAudit != nil {
			c.capabilityAudit.RecordRoute(false, false)
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
	c.capCachedTools, c.capCacheKeyOK = capability.LoadCachedToolsForSpecs(specs)
	c.semanticRouter = router
	c.capabilityAudit = audit
}

// SetCapabilityProxyRouting directs unready MCP route candidates to
// use_capability instead of connect_tool_source. Used by Delivery and by
// Balanced dual-model Planner boots.
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
