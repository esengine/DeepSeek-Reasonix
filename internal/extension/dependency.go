package extension

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"reasonix/internal/extensioncontract"
)

// ComponentID is the stable identity of one lifecycle component (initially one
// native v2 sidecar runtime package, plus host-owned nodes).
type ComponentID string

// ComponentState is the fixed lifecycle state machine.
type ComponentState string

const (
	ComponentInactive  ComponentState = "Inactive"
	ComponentPreparing ComponentState = "Preparing"
	ComponentActive    ComponentState = "Active"
	ComponentDraining  ComponentState = "Draining"
	ComponentFailed    ComponentState = "Failed"
)

// ComponentDescriptor is the immutable description of one component used to
// build the dependency graph. It must not carry live handles.
type ComponentDescriptor struct {
	ID         ComponentID
	Source     ContributionSource
	Requires   []extensioncontract.Requirement
	Provides   []extensioncontract.Capability
	Intercepts []InterceptorPoint
	Replaces   []Slot
	// Priority participates in deterministic activation ordering.
	Priority int
	// Optional marks the whole component as non-blocking when it cannot activate.
	Optional bool
}

// ComponentEpoch is the dependency identity that forces consumer reload when
// it changes. Fiber UIDs alone are not enough.
type ComponentEpoch struct {
	CapabilityKey       extensioncontract.CapabilityKey
	ProviderComponentID ComponentID
	ProviderVersion     string
	ProviderSchemaHash  string
}

// String returns a stable epoch fingerprint.
func (e ComponentEpoch) String() string {
	return fmt.Sprintf("%s|%s|%s|%s", e.CapabilityKey.String(), e.ProviderComponentID, e.ProviderVersion, e.ProviderSchemaHash)
}

// DependencyGraph is the resolved capability graph for one generation.
type DependencyGraph struct {
	Components map[ComponentID]ComponentDescriptor
	// Edges maps consumer → providers it depends on.
	Edges map[ComponentID][]ComponentID
	// Providers maps capability key string → component IDs that provide it.
	Providers map[string][]ComponentID
	// Diagnostics collects optional-missing and non-fatal notes.
	Diagnostics []string
}

// GraphError is a hard dependency resolution failure.
type GraphError struct {
	Reason string
	Cycle  []ComponentID
	Detail string
}

func (e *GraphError) Error() string {
	if e == nil {
		return ""
	}
	if len(e.Cycle) > 0 {
		parts := make([]string, len(e.Cycle))
		for i, id := range e.Cycle {
			parts[i] = string(id)
		}
		return fmt.Sprintf("extension: %s: %s", e.Reason, strings.Join(parts, " -> "))
	}
	if e.Detail != "" {
		return fmt.Sprintf("extension: %s: %s", e.Reason, e.Detail)
	}
	return "extension: " + e.Reason
}

// BuildDependencyGraph validates descriptors, resolves requirements, detects
// required cycles, and records optional-missing diagnostics.
func BuildDependencyGraph(components []ComponentDescriptor) (*DependencyGraph, error) {
	g := &DependencyGraph{
		Components: make(map[ComponentID]ComponentDescriptor, len(components)),
		Edges:      make(map[ComponentID][]ComponentID),
		Providers:  make(map[string][]ComponentID),
	}
	for _, c := range components {
		if c.ID == "" {
			return nil, &GraphError{Reason: "invalid_component", Detail: "empty component id"}
		}
		if _, dup := g.Components[c.ID]; dup {
			return nil, &GraphError{Reason: "duplicate_component", Detail: string(c.ID)}
		}
		for _, p := range c.Provides {
			if err := p.Validate(); err != nil {
				return nil, &GraphError{Reason: "invalid_capability", Detail: err.Error()}
			}
			key := p.Key.String()
			g.Providers[key] = append(g.Providers[key], c.ID)
		}
		for _, r := range c.Requires {
			if err := r.Validate(); err != nil {
				return nil, &GraphError{Reason: "invalid_requirement", Detail: err.Error()}
			}
		}
		g.Components[c.ID] = c
	}

	// Sort provider lists for determinism.
	for k, ids := range g.Providers {
		slices.Sort(ids)
		g.Providers[k] = ids
	}

	for _, c := range components {
		for _, req := range c.Requires {
			key := req.Key.String()
			candidates := g.Providers[key]
			var matched []ComponentID
			for _, pid := range candidates {
				prov := g.Components[pid]
				if slices.ContainsFunc(prov.Provides, func(cap extensioncontract.Capability) bool {
					return req.SatisfiedBy(cap)
				}) {
					matched = append(matched, pid)
				}
			}
			if len(matched) == 0 {
				if req.Optional {
					g.Diagnostics = append(g.Diagnostics, fmt.Sprintf("optional dependency unsatisfied: %s requires %s", c.ID, key))
					continue
				}
				return nil, &GraphError{
					Reason: "dependency_unsatisfied",
					Detail: fmt.Sprintf("%s requires %s", c.ID, key),
				}
			}
			if len(matched) > 1 {
				// Multiple providers for the same key without explicit selection.
				parts := make([]string, len(matched))
				for i, id := range matched {
					parts[i] = string(id)
				}
				return nil, &GraphError{
					Reason: "duplicate_provider",
					Detail: fmt.Sprintf("%s: providers %s", key, strings.Join(parts, ", ")),
				}
			}
			g.Edges[c.ID] = append(g.Edges[c.ID], matched[0])
		}
		// Deterministic edge order.
		if edges := g.Edges[c.ID]; len(edges) > 1 {
			slices.Sort(edges)
			g.Edges[c.ID] = edges
		}
	}

	if cycle := detectRequiredCycle(g); len(cycle) > 0 {
		return nil, &GraphError{Reason: "dependency_cycle", Cycle: cycle}
	}
	slices.Sort(g.Diagnostics)
	return g, nil
}

// ActivateOrder returns the deterministic topological activation order.
func (g *DependencyGraph) ActivateOrder() []ComponentID {
	if g == nil {
		return nil
	}
	return topoOrder(g, false)
}

// DrainOrder returns reverse topological order for draining.
func (g *DependencyGraph) DrainOrder() []ComponentID {
	if g == nil {
		return nil
	}
	return topoOrder(g, true)
}

// EpochFor returns the epoch identity a consumer should pin for req.
func (g *DependencyGraph) EpochFor(consumer ComponentID, req extensioncontract.Requirement) (ComponentEpoch, bool) {
	if g == nil {
		return ComponentEpoch{}, false
	}
	for _, pid := range g.Edges[consumer] {
		prov := g.Components[pid]
		for _, cap := range prov.Provides {
			if req.SatisfiedBy(cap) {
				return ComponentEpoch{
					CapabilityKey:       cap.Key,
					ProviderComponentID: pid,
					ProviderVersion:     cap.Version,
					ProviderSchemaHash:  cap.SchemaHash,
				}, true
			}
		}
	}
	return ComponentEpoch{}, false
}

func detectRequiredCycle(g *DependencyGraph) []ComponentID {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[ComponentID]int, len(g.Components))
	var stack []ComponentID
	var cycle []ComponentID

	var dfs func(ComponentID) bool
	dfs = func(n ComponentID) bool {
		color[n] = gray
		stack = append(stack, n)
		for _, m := range g.Edges[n] {
			switch color[m] {
			case gray:
				// Extract cycle from stack.
				for _, id := range slices.Backward(stack) {
					cycle = append([]ComponentID{id}, cycle...)
					if id == m {
						break
					}
				}
				cycle = append(cycle, m)
				return true
			case white:
				if dfs(m) {
					return true
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
		return false
	}

	ids := make([]ComponentID, 0, len(g.Components))
	for id := range g.Components {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if color[id] == white {
			if dfs(id) {
				return cycle
			}
		}
	}
	return nil
}

func topoOrder(g *DependencyGraph, reverse bool) []ComponentID {
	// Kahn's algorithm with deterministic ready-set ordering.
	indeg := make(map[ComponentID]int, len(g.Components))
	// Build reverse adjacency: provider → consumers (activation needs providers first).
	// Edges are consumer → provider, so provider must activate before consumer.
	consumersOf := make(map[ComponentID][]ComponentID)
	for id := range g.Components {
		indeg[id] = 0
	}
	for consumer, providers := range g.Edges {
		indeg[consumer] = len(providers)
		for _, p := range providers {
			consumersOf[p] = append(consumersOf[p], consumer)
		}
	}
	for p, list := range consumersOf {
		slices.Sort(list)
		consumersOf[p] = list
	}

	var ready []ComponentID
	for id, d := range indeg {
		if d == 0 {
			ready = append(ready, id)
		}
	}
	sortReady := func() {
		sort.SliceStable(ready, func(i, j int) bool {
			return componentLess(g, ready[i], ready[j])
		})
	}
	sortReady()

	var order []ComponentID
	for len(ready) > 0 {
		n := ready[0]
		ready = ready[1:]
		order = append(order, n)
		for _, c := range consumersOf[n] {
			indeg[c]--
			if indeg[c] == 0 {
				ready = append(ready, c)
				sortReady()
			}
		}
	}
	if reverse {
		for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 {
			order[i], order[j] = order[j], order[i]
		}
	}
	return order
}

// componentLess implements the fixed sort: dependency rank, scope rank,
// priority, canonical component ID. Dependency rank is approximated by
// number of transitive providers (deeper deps first in activation).
func componentLess(g *DependencyGraph, a, b ComponentID) bool {
	ra, rb := dependencyRank(g, a), dependencyRank(g, b)
	if ra != rb {
		return ra < rb
	}
	sa, sb := tierRank(g.Components[a].Source.Scope), tierRank(g.Components[b].Source.Scope)
	if sa != sb {
		// Higher scope rank first so project-owned nodes win ties predictably.
		return sa > sb
	}
	pa, pb := g.Components[a].Priority, g.Components[b].Priority
	if pa != pb {
		return pa > pb
	}
	return a < b
}

func dependencyRank(g *DependencyGraph, id ComponentID) int {
	seen := map[ComponentID]bool{}
	var walk func(ComponentID) int
	walk = func(n ComponentID) int {
		if seen[n] {
			return 0
		}
		seen[n] = true
		max := 0
		for _, p := range g.Edges[n] {
			if d := walk(p) + 1; d > max {
				max = d
			}
		}
		return max
	}
	return walk(id)
}
