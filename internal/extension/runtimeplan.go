package extension

import (
	"maps"
	"slices"
	"strings"
)

// SubgraphKind classifies which assembly subgraph a RuntimePlan touches so
// Rebuild can skip unaffected work (tools/prompt, interceptors, UI, MCP).
type SubgraphKind uint8

const (
	// SubgraphNone means no component identity changed (full no-op).
	SubgraphNone SubgraphKind = iota
	// SubgraphInterceptorOnly means only interceptor contributions moved.
	SubgraphInterceptorOnly
	// SubgraphProviderOnly means only provider capabilities moved.
	SubgraphProviderOnly
	// SubgraphUIOnly means only UI capabilities moved.
	SubgraphUIOnly
	// SubgraphMCPOnly means only MCP-related components moved.
	SubgraphMCPOnly
	// SubgraphSidecar means native runtime packages changed without a single kind.
	SubgraphSidecar
	// SubgraphFull means mixed or host-wide changes; full rebuild is required.
	SubgraphFull
)

// RuntimePlan describes the transition from one generation's graph/snapshot
// to the next. Builders and Rebuild consume it to activate only the affected
// subgraph. PrefixChanged is an observed post-build fact; graph diffing alone
// only classifies which work may be required.
type RuntimePlan struct {
	FromGeneration uint64
	ToGeneration   uint64
	Added          []ComponentID
	Removed        []ComponentID
	Reloaded       []ComponentID
	Unchanged      []ComponentID
	ActivateOrder  []ComponentID
	DrainOrder     []ComponentID
	// PrefixChanged reports whether the frozen provider-visible prefix actually
	// changed between the previous and current snapshots. Boot sets it only
	// after both snapshots exist and their CacheHash values can be compared.
	PrefixChanged bool
	// ProviderChanged reports whether any changed component provided a provider
	// capability on either side of the transition. Looking at both graphs keeps
	// provider removal visible to diagnostics.
	ProviderChanged bool
	// Kind is the classified subgraph of this plan (computed by DiffRuntimePlan).
	Kind SubgraphKind
	// Graph is the resolved target graph (may be nil for pure no-op plans).
	Graph *DependencyGraph
}

// IsNoOp reports whether the plan changes no components.
func (p *RuntimePlan) IsNoOp() bool {
	if p == nil {
		return true
	}
	return len(p.Added) == 0 && len(p.Removed) == 0 && len(p.Reloaded) == 0
}

// MayChangePrefix is the conservative pre-build planning signal for whether a
// rebuild must re-evaluate provider-visible prompt/tool prefix state. It is not
// a diagnostic fact; PrefixChanged is set after comparing frozen snapshots.
// Interceptor-only and UI-only plans do not change CacheHash by contract.
func (p *RuntimePlan) MayChangePrefix() bool {
	if p == nil || p.IsNoOp() {
		return false
	}
	switch p.Kind {
	case SubgraphInterceptorOnly, SubgraphUIOnly:
		return false
	default:
		return true
	}
}

// AffectsSidecars reports whether any native runtime package must start/drain.
func (p *RuntimePlan) AffectsSidecars() bool {
	if p == nil || p.IsNoOp() {
		return false
	}
	return true
}

// AffectsInterceptors reports whether the interceptor chain must rebuild.
func (p *RuntimePlan) AffectsInterceptors() bool {
	if p == nil || p.IsNoOp() {
		return false
	}
	return p.Kind == SubgraphInterceptorOnly || p.Kind == SubgraphFull || p.Kind == SubgraphSidecar
}

// AffectsUI reports whether the extension UI hub must rebind.
func (p *RuntimePlan) AffectsUI() bool {
	if p == nil || p.IsNoOp() {
		return false
	}
	return p.Kind == SubgraphUIOnly || p.Kind == SubgraphFull || p.Kind == SubgraphSidecar
}

// AffectsProviders reports whether extension-hosted providers must re-merge.
func (p *RuntimePlan) AffectsProviders() bool {
	if p == nil || p.IsNoOp() {
		return false
	}
	return p.ProviderChanged || p.Kind == SubgraphProviderOnly || p.Kind == SubgraphFull || p.Kind == SubgraphSidecar
}

// DiffRuntimePlan compares two graphs and produces a deterministic plan.
// from may be nil (cold start). PrefixChanged remains false here because graph
// identity cannot prove provider-visible byte changes; boot observes it after
// the next RuntimeSnapshot has been frozen.
func DiffRuntimePlan(from, to *DependencyGraph, fromGen, toGen uint64) *RuntimePlan {
	plan := &RuntimePlan{
		FromGeneration: fromGen,
		ToGeneration:   toGen,
		Graph:          to,
	}
	if to == nil {
		return plan
	}
	fromIDs := map[ComponentID]ComponentDescriptor{}
	if from != nil {
		maps.Copy(fromIDs, from.Components)
	}
	toIDs := to.Components

	var added, removed, reloaded, unchanged []ComponentID
	for id, neo := range toIDs {
		old, ok := fromIDs[id]
		if !ok {
			added = append(added, id)
			continue
		}
		if componentIdentityChanged(old, neo) || epochsChanged(from, to, id) {
			reloaded = append(reloaded, id)
			continue
		}
		unchanged = append(unchanged, id)
	}
	for id := range fromIDs {
		if _, ok := toIDs[id]; !ok {
			removed = append(removed, id)
		}
	}
	sortIDs(added)
	sortIDs(removed)
	sortIDs(reloaded)
	sortIDs(unchanged)
	plan.Added = added
	plan.Removed = removed
	plan.Reloaded = reloaded
	plan.Unchanged = unchanged
	plan.ActivateOrder = to.ActivateOrder()
	// Drain only removed + reloaded, in reverse dependency order of the old graph.
	drainSet := map[ComponentID]bool{}
	for _, id := range removed {
		drainSet[id] = true
	}
	for _, id := range reloaded {
		drainSet[id] = true
	}
	if from != nil {
		for _, id := range from.DrainOrder() {
			if drainSet[id] {
				plan.DrainOrder = append(plan.DrainOrder, id)
			}
		}
	} else {
		plan.DrainOrder = append(plan.DrainOrder, removed...)
		plan.DrainOrder = append(plan.DrainOrder, reloaded...)
	}
	plan.Kind = classifySubgraph(plan, from, to)
	plan.ProviderChanged = changedComponentsProvideKind(plan, from, to, "provider")
	return plan
}

// classifySubgraph inspects changed components' provides/intercepts to pick
// the narrowest rebuild subgraph.
func classifySubgraph(plan *RuntimePlan, from, to *DependencyGraph) SubgraphKind {
	if plan == nil || plan.IsNoOp() {
		return SubgraphNone
	}
	changed := append(append(append([]ComponentID{}, plan.Added...), plan.Removed...), plan.Reloaded...)
	var hasInterceptor, hasProvider, hasUI, hasMCP, hasOther bool
	mcpSchemaChanged := false
	for _, id := range changed {
		oldDesc, oldOK := graphComponent(from, id)
		newDesc, newOK := graphComponent(to, id)
		mcpSchemaChanged = mcpSchemaChanged || mcpCapabilitySchemaChanged(oldDesc, oldOK, newDesc, newOK)
		componentClassified := false
		for _, desc := range []ComponentDescriptor{oldDesc, newDesc} {
			if len(desc.Intercepts) > 0 || len(desc.Replaces) > 0 {
				hasInterceptor = true
				componentClassified = true
			}
			for _, cap := range desc.Provides {
				switch strings.ToLower(cap.Key.Kind) {
				case "provider":
					hasProvider = true
					componentClassified = true
				case "ui", "uiaction":
					hasUI = true
					componentClassified = true
				case "mcp", "mcpserver":
					hasMCP = true
					componentClassified = true
				case "interceptors", "strategies":
					hasInterceptor = true
					componentClassified = true
				default:
					if cap.Key.Kind != "" {
						hasOther = true
						componentClassified = true
					}
				}
			}
		}
		// Plugin components with empty provides still count as sidecar.
		if strings.HasPrefix(string(id), "plugin/") && !componentClassified {
			hasOther = true
		}
	}
	// An MCP backend roll may stay narrow only while its declared schema shape is
	// unchanged. Added/removed/renamed schemas need a full snapshot rebuild so
	// provider-visible tool bytes cannot remain stale.
	if mcpSchemaChanged {
		return SubgraphFull
	}
	kinds := 0
	if hasInterceptor {
		kinds++
	}
	if hasProvider {
		kinds++
	}
	if hasUI {
		kinds++
	}
	if hasMCP {
		kinds++
	}
	if hasOther {
		kinds++
	}
	if kinds > 1 {
		if hasOther {
			return SubgraphFull
		}
		return SubgraphSidecar
	}
	switch {
	case hasInterceptor:
		return SubgraphInterceptorOnly
	case hasProvider:
		return SubgraphProviderOnly
	case hasUI:
		return SubgraphUIOnly
	case hasMCP:
		return SubgraphMCPOnly
	default:
		return SubgraphFull
	}
}

func graphComponent(graph *DependencyGraph, id ComponentID) (ComponentDescriptor, bool) {
	if graph == nil {
		return ComponentDescriptor{}, false
	}
	desc, ok := graph.Components[id]
	return desc, ok
}

func changedComponentsProvideKind(plan *RuntimePlan, from, to *DependencyGraph, kind string) bool {
	if plan == nil || plan.IsNoOp() {
		return false
	}
	changed := append(append(append([]ComponentID{}, plan.Added...), plan.Removed...), plan.Reloaded...)
	for _, id := range changed {
		oldDesc, _ := graphComponent(from, id)
		newDesc, _ := graphComponent(to, id)
		for _, desc := range []ComponentDescriptor{oldDesc, newDesc} {
			for _, capability := range desc.Provides {
				if strings.EqualFold(strings.TrimSpace(capability.Key.Kind), kind) {
					return true
				}
			}
		}
	}
	return false
}

func mcpCapabilitySchemaChanged(oldDesc ComponentDescriptor, oldOK bool, newDesc ComponentDescriptor, newOK bool) bool {
	oldShape := mcpCapabilityShape(oldDesc, oldOK)
	newShape := mcpCapabilityShape(newDesc, newOK)
	if len(oldShape) != len(newShape) {
		return len(oldShape) > 0 || len(newShape) > 0
	}
	for key, oldHash := range oldShape {
		if newHash, ok := newShape[key]; !ok || newHash != oldHash {
			return true
		}
	}
	return false
}

func mcpCapabilityShape(desc ComponentDescriptor, ok bool) map[string]string {
	shape := map[string]string{}
	if !ok {
		return shape
	}
	for _, capability := range desc.Provides {
		switch strings.ToLower(strings.TrimSpace(capability.Key.Kind)) {
		case "mcp", "mcpserver":
			shape[capability.Key.String()] = strings.TrimSpace(capability.SchemaHash)
		}
	}
	return shape
}

func componentIdentityChanged(a, b ComponentDescriptor) bool {
	if a.Priority != b.Priority || a.Optional != b.Optional {
		return true
	}
	if a.Source.key() != b.Source.key() || a.Source.Version != b.Source.Version {
		return true
	}
	if !slices.Equal(a.Intercepts, b.Intercepts) || !slices.Equal(a.Replaces, b.Replaces) {
		return true
	}
	if len(a.Provides) != len(b.Provides) || len(a.Requires) != len(b.Requires) {
		return true
	}
	for i := range a.Provides {
		if a.Provides[i].CanonicalHash() != b.Provides[i].CanonicalHash() {
			return true
		}
	}
	for i := range a.Requires {
		if a.Requires[i].Key != b.Requires[i].Key ||
			a.Requires[i].Version != b.Requires[i].Version ||
			a.Requires[i].VersionRange != b.Requires[i].VersionRange ||
			a.Requires[i].SchemaHash != b.Requires[i].SchemaHash ||
			a.Requires[i].Optional != b.Requires[i].Optional {
			return true
		}
	}
	return false
}

func epochsChanged(from, to *DependencyGraph, id ComponentID) bool {
	if from == nil || to == nil {
		return false
	}
	c := to.Components[id]
	for _, req := range c.Requires {
		e1, ok1 := from.EpochFor(id, req)
		e2, ok2 := to.EpochFor(id, req)
		if ok1 != ok2 || e1.String() != e2.String() {
			return true
		}
	}
	return false
}

func sortIDs(ids []ComponentID) {
	slices.Sort(ids)
}
