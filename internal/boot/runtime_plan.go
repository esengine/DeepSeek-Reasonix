package boot

import (
	"slices"
	"strings"

	"reasonix/internal/browserhost"
	"reasonix/internal/config"
	"reasonix/internal/extension"
	"reasonix/internal/extension/dispatch"
	"reasonix/internal/extension/sidecar"
	"reasonix/internal/extensioncontract"
)

func sortComponentIDs(ids []extension.ComponentID) {
	slices.Sort(ids)
}

// RuntimeReload is previous-generation state for incremental sidecar adoption
// and subgraph-classified rebuild.
type RuntimeReload struct {
	// ForceFullRebuild bypasses extension subgraph reuse when config-owned
	// skills/providers may have changed without changing the native graph.
	ForceFullRebuild bool
	Extensions       *sidecar.Manager
	Graph            *extension.DependencyGraph
	Generation       uint64
	// Owner is reused across one controller/session rebuild lineage. Cold builds
	// leave it nil and receive a fresh isolated owner.
	Owner *extension.RuntimeOwner
	// PreviousSnapshot/Dispatcher enable CacheHash-stable no-op rebuilds and
	// interceptor-only rewire without rediscovering the whole assembly.
	PreviousSnapshot   *extension.RuntimeSnapshot
	PreviousDispatcher *dispatch.Dispatcher
	PreviousPlan       *extension.RuntimePlan
	// ReuseAssembly, when set with a compatible plan, skips skill/command/hook
	// rediscovery inside BuildRuntime.
	ReuseAssembly *ReusedAssembly
}

// hostProvidesForOptions returns the synthetic host capabilities for this
// frontend. Browser companion is advertised iff BrowserHost != nil.
func hostProvidesForOptions(opts Options) []extensioncontract.Capability {
	if opts.BrowserHost == nil {
		return nil
	}
	return []extensioncontract.Capability{browserhost.Capability()}
}

// buildRuntimeGraph constructs the dependency graph for installed native
// runtime packages. Host-provided capabilities (if any) are included as a
// synthetic "host" component so plugin requirements can resolve.
func buildRuntimeGraph(home string, hostProvides []extensioncontract.Capability) (*extension.DependencyGraph, error) {
	packages, _ := sidecar.LoadRuntimePackages(home)
	comps := make([]extension.ComponentDescriptor, 0, len(packages)+1)
	if len(hostProvides) > 0 {
		comps = append(comps, extension.ComponentDescriptor{
			ID:       "host",
			Source:   extension.ContributionSource{Scope: extension.ScopeBuiltin, Origin: "host"},
			Provides: hostProvides,
		})
	}
	for _, item := range packages {
		pkg := item.Package
		id := extension.ComponentID("plugin/" + pkg.Manifest.Name)
		var intercepts []extension.InterceptorPoint
		var replaces []extension.Slot
		priority := 0
		optional := true
		if rt := pkg.Manifest.Runtime; rt != nil {
			priority = rt.Priority
			optional = !rt.Required
			for _, p := range rt.Intercepts {
				intercepts = append(intercepts, extension.InterceptorPoint(p))
			}
			for _, s := range rt.Replaces {
				if slot, err := extension.ParseSlot(s); err == nil {
					replaces = append(replaces, slot)
				}
			}
		}
		comps = append(comps, extension.ComponentDescriptor{
			ID:         id,
			Source:     extension.ContributionSource{Scope: extension.ScopePlugin, PluginID: pkg.Manifest.Name, Origin: "plugin", Version: pkg.Manifest.Version},
			Requires:   pkg.Requires(),
			Provides:   pkg.ProvidesCapabilities(),
			Intercepts: intercepts,
			Replaces:   replaces,
			Priority:   priority,
			Optional:   optional,
		})
	}
	if len(comps) == 0 {
		return &extension.DependencyGraph{
			Components: map[extension.ComponentID]extension.ComponentDescriptor{},
			Edges:      map[extension.ComponentID][]extension.ComponentID{},
			Providers:  map[string][]extension.ComponentID{},
		}, nil
	}
	return extension.BuildDependencyGraph(comps)
}

// attachPlanAndStatus fills BuildResult.Plan and Status from graphs using the
// lifecycle registry so doctor can explain Inactive/Failed components.
func attachPlanAndStatus(res *BuildResult, from *extension.DependencyGraph, to *extension.DependencyGraph, fromGen uint64, previousSnapshot *extension.RuntimeSnapshot) {
	if res == nil {
		return
	}
	toGen := uint64(0)
	if res.Snapshot != nil {
		toGen = res.Snapshot.Generation()
	}
	plan := extension.DiffRuntimePlan(from, to, fromGen, toGen)
	if previousSnapshot != nil && res.Snapshot != nil {
		plan.PrefixChanged = previousSnapshot.CacheHash() != res.Snapshot.CacheHash()
	}
	res.Plan = plan
	life := extension.NewLifecycleRegistry(toGen)
	status := &extension.RuntimeStatus{
		PublishedGeneration: toGen,
		Plan:                extension.PlanView(plan),
	}
	if res.Runtime != nil {
		status.Receipts = res.Runtime.Receipts()
	}
	if to != nil {
		// Report both Active (ActivateOrder) and graph-Inactive components.
		// Deterministic order: ActivateOrder first, then remaining sorted.
		seen := map[extension.ComponentID]bool{}
		ordered := append([]extension.ComponentID(nil), to.ActivateOrder()...)
		for _, id := range ordered {
			seen[id] = true
		}
		var rest []extension.ComponentID
		for id := range to.Components {
			if !seen[id] {
				rest = append(rest, id)
			}
		}
		sortComponentIDs(rest)
		ordered = append(ordered, rest...)
		for _, id := range ordered {
			life.Ensure(id)
			_ = life.Transition(id, extension.ComponentPreparing, "")
			if reasons := to.InactiveReasons(id); len(reasons) > 0 {
				// One diagnostic entry per reason for doctor multi-line output.
				for _, reason := range reasons {
					_ = life.Transition(id, extension.ComponentInactive, reason)
				}
				continue
			}
			if desc, ok := to.Components[id]; ok {
				// Declared provides with no live client → Unavailable diagnostic.
				if res.Extensions != nil && strings.HasPrefix(string(id), "plugin/") {
					name := sidecar.PluginNameFromComponentID(id)
					if name != "" && res.Extensions.Client(name) == nil && len(desc.Provides) > 0 {
						var diags []string
						for _, cap := range desc.Provides {
							diags = append(diags, "capability Unavailable: "+cap.Key.String()+" (runtime client not started)")
						}
						_ = life.Transition(id, extension.ComponentInactive, strings.Join(diags, "; "))
						continue
					}
				}
			}
			if res.Extensions != nil && strings.HasPrefix(string(id), "plugin/") {
				name := sidecar.PluginNameFromComponentID(id)
				if name != "" && res.Extensions.Client(name) == nil {
					_ = life.Transition(id, extension.ComponentInactive, "runtime client not started")
				} else {
					_ = life.Transition(id, extension.ComponentActive, "")
				}
			} else {
				_ = life.Transition(id, extension.ComponentActive, "")
			}
		}
		status.Components = life.All()
	}
	res.Status = status
	res.Lifecycle = life
}

// planForPreflight builds the RuntimePlan used to adopt Unchanged sidecars.
// Cold start always builds a plan so Inactive browser plugins are never
// launched, even when there is no previous Manager.
func planForPreflight(opts Options, toGen uint64) *extension.RuntimePlan {
	hostCaps := hostProvidesForOptions(opts)
	toGraph, err := buildRuntimeGraph(config.ReasonixHomeDir(), hostCaps)
	if err != nil {
		return nil
	}
	return extension.DiffRuntimePlan(opts.Graph, toGraph, opts.Generation, toGen)
}

// finalizeBuildResult attaches the cold-start RuntimePlan and status, then
// publishes the generation so stale traffic from prior runtimes is dropped.
func finalizeBuildResult(res *BuildResult, publish bool) *BuildResult {
	if res == nil {
		return nil
	}
	hostCaps := []extensioncontract.Capability(nil)
	if res.BrowserHost != nil {
		hostCaps = []extensioncontract.Capability{browserhost.Capability()}
	}
	if graph, err := buildRuntimeGraph(config.ReasonixHomeDir(), hostCaps); err == nil {
		attachPlanAndStatus(res, nil, graph, 0, nil)
	}
	if publish {
		publishBuildResult(res)
	}
	return res
}

func publishBuildResult(res *BuildResult) {
	if res == nil || res.Snapshot == nil {
		return
	}
	gen := res.Snapshot.Generation()
	if gen == 0 {
		return
	}
	owner := res.Owner
	if owner == nil {
		owner = extension.RuntimeOwnerOrDefault(nil)
		res.Owner = owner
	}
	// Ensure every live client has the generation-scoped browser binding
	// before publish so cold start and full rebuilds can call immediately.
	// Subgraph commit already swapped handlers; rebinding is idempotent.
	bindBrowserHandlers(res.Extensions, res.BrowserHost, owner, gen)
	gate := owner.Gate
	// Expire any previous drain TTLs before publishing the new generation.
	_ = gate.SweepAndForceExpire()
	gate.Publish(gen)
	// Product path: force-expire old generations after drainTTL so in-flight
	// work cannot linger forever after rebuild.
	gate.ScheduleDrainWatch()
	if res.Controller != nil {
		res.Controller.SetRuntimeGeneration(gen)
	}
	if res.Runtime != nil {
		owner.Receipts.IngestScope(res.Runtime.Scope())
	}
	if res.Status != nil {
		res.Status.PublishedGeneration = gen
	}
}
