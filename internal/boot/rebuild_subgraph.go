package boot

import (
	"context"
	"fmt"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/extension"
	"reasonix/internal/extension/dispatch"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/extension/sidecar"
	"reasonix/internal/extension/uihub"
	"reasonix/internal/provider"
)

// tryRebuildSubgraph patches narrow plans without BuildRuntime (fail-atomic).
// Callers must skip Close when BuildResult.ReusedController is set.
func tryRebuildSubgraph(ctx context.Context, old *control.Controller, previous *BuildResult, opts Options, m runtimeMigration) (res *BuildResult, handled bool, err error) {
	if previous == nil || previous.Snapshot == nil || old == nil {
		return nil, false, nil
	}
	start := time.Now()
	from := opts.Graph
	if previous.Plan != nil && previous.Plan.Graph != nil {
		from = previous.Plan.Graph
	}
	graphStart := time.Now()
	to, gerr := buildRuntimeGraph(config.ReasonixHomeDir(), nil)
	extension.DefaultLifecycleMetrics.ObserveGraphBuild(time.Since(graphStart))
	if gerr != nil {
		return nil, false, nil
	}
	diffStart := time.Now()
	plan := extension.DiffRuntimePlan(from, to, opts.Generation, 0)
	extension.DefaultLifecycleMetrics.ObservePlanDiff(time.Since(diffStart))
	if plan == nil {
		return nil, false, nil
	}

	switch plan.Kind {
	case extension.SubgraphNone:
		extension.DefaultLifecycleMetrics.NoOpRebuilds.Add(1)
	case extension.SubgraphInterceptorOnly, extension.SubgraphUIOnly, extension.SubgraphProviderOnly, extension.SubgraphMCPOnly:
		extension.DefaultLifecycleMetrics.SubgraphRebuilds.Add(1)
	default:
		extension.DefaultLifecycleMetrics.FullRebuilds.Add(1)
		return nil, false, nil
	}

	gen := nextRuntimeGeneration()
	plan.ToGeneration = gen
	plan.Graph = to

	// Checkpoint live controller bindings for fail-atomic restore.
	prevDispatcher := previous.Dispatcher
	prevResolver := previous.ProviderResolver
	prevUI := previous.ExtensionUI
	prevUISession := controllerSessionID(previous.Controller)
	prevUIGen := previous.Snapshot.Generation()

	res = &BuildResult{
		Controller:           previous.Controller,
		Snapshot:             previous.Snapshot.WithGeneration(gen),
		Runtime:              extension.NewRuntimeSet(gen),
		Owner:                opts.Owner,
		Extensions:           previous.Extensions,
		Dispatcher:           previous.Dispatcher,
		ExtensionUI:          previous.ExtensionUI,
		ProviderResolver:     previous.ProviderResolver,
		BaseProviderResolver: previous.BaseProviderResolver,
		Assembly:             previous.Assembly,
		Plan:                 plan,
		ReusedController:     true,
	}
	session := protocol.SessionContext{
		SessionID:     controllerSessionID(previous.Controller),
		WorkspaceRoot: previous.Controller.WorkspaceRoot(),
		Generation:    gen,
	}
	if session.SessionID == "" {
		session.SessionID = "session"
	}
	if session.WorkspaceRoot == "" {
		session.WorkspaceRoot = "."
	}

	oldMgr := previous.Extensions
	fail := func(stageErr error) (*BuildResult, bool, error) {
		// Restore controller to pre-patch bindings (no partial commit should remain).
		restoreControllerBindings(previous.Controller, prevDispatcher, prevResolver, prevUI, prevUISession, prevUIGen, oldMgr)
		if res.Runtime != nil {
			_ = res.Runtime.Close()
		}
		if res.Extensions != nil && res.Extensions != oldMgr {
			res.Extensions.RollbackPlanStart(oldMgr)
		}
		return nil, true, stageErr
	}

	var patchErr error
	switch plan.Kind {
	case extension.SubgraphNone:
		if res.Extensions != nil {
			_ = res.Runtime.Track(extension.Effect{
				ID: "sidecar-manager-adopted", Owner: "boot", Class: extension.Cancelable,
				Dispose: func(context.Context) error { return nil },
			})
		}
	case extension.SubgraphUIOnly, extension.SubgraphInterceptorOnly, extension.SubgraphProviderOnly, extension.SubgraphMCPOnly:
		// Stage only: does not mutate controller dispatcher/resolver/UI.
		patchErr = stageSidecarSubgraph(ctx, res, oldMgr, plan, session, gen)
	}
	if patchErr != nil {
		return fail(patchErr)
	}

	if err := awaitSidecarsReady(ctx, res.Extensions); err != nil {
		return fail(err)
	}

	// Commit after staging + ready. If this panics mid-way, fail restores.
	if err := commitControllerExtPatch(res, session, gen); err != nil {
		return fail(err)
	}

	_ = m
	attachPlanAndStatus(res, from, to, opts.Generation, previous.Snapshot)

	if prevGen := previous.Snapshot.Generation(); prevGen != 0 && prevGen != gen {
		registerControllerDrainCancel(res.Owner, prevGen, old)
		if !res.ReusedController {
			if host := old.Host(); host != nil {
				h := host
				res.Owner.Gate.RegisterDrainCancel(prevGen, func() { h.CancelInFlightMCP() })
			}
		}
	}
	finishRebuildPublish(res, nil, start)
	if oldMgr != nil && res.Extensions != oldMgr && res.Plan != nil {
		drainStart := time.Now()
		oldMgr.DrainPlan(res.Plan)
		extension.DefaultLifecycleMetrics.ObserveDrain(time.Since(drainStart))
	}
	return res, true, nil
}

func restoreControllerBindings(ctrl *control.Controller, disp *dispatch.Dispatcher, resolver provider.Resolver, ui *uihub.Hub, uiSession string, uiGen uint64, oldMgr *sidecar.Manager) {
	if ctrl == nil {
		return
	}
	if disp != nil {
		ctrl.ReplaceExtensions(disp)
	}
	ctrl.SetProviderResolver(resolver)
	if ui != nil {
		ui.BindGeneration(uiSession, uiGen)
		if oldMgr != nil {
			bindExtensionUI(ui, oldMgr, func(string) {})
		}
		ctrl.SetExtensionUI(ui)
	}
}

func (res *BuildResult) ensureRuntime(gen uint64) *extension.RuntimeSet {
	if res == nil {
		return nil
	}
	if res.Runtime == nil || res.Runtime.Generation() != gen {
		res.Runtime = extension.NewRuntimeSet(gen)
	}
	return res.Runtime
}

// stageSidecarSubgraph prepares manager/snapshot/dispatcher/resolver without
// mutating live controller bindings. BindGeneration and stream-router install
// wait for commit; staged-generation host/ui/* is dropped until then.
func stageSidecarSubgraph(ctx context.Context, res *BuildResult, oldMgr *sidecar.Manager, plan *extension.RuntimePlan, session protocol.SessionContext, gen uint64) error {
	home := config.ReasonixHomeDir()
	var ui sidecar.UIHandler
	if res.ExtensionUI != nil {
		ui = res.ExtensionUI
	}
	mgr, _, err := sidecar.StartPackagesWithPlan(ctx, home, session, ui, oldMgr, plan)
	if err != nil {
		if mgr != nil {
			mgr.RollbackPlanStart(oldMgr)
		}
		return err
	}
	res.Extensions = mgr
	rs := res.ensureRuntime(gen)
	closeOnDispose := mgr != oldMgr
	if err := rs.Track(extension.Effect{
		ID:        "sidecar-manager",
		Owner:     "boot",
		Component: "extension-runtimes",
		Class:     extension.Cancelable,
		Dispose: func(context.Context) error {
			if closeOnDispose {
				return mgr.Close()
			}
			return nil
		},
	}); err != nil {
		if closeOnDispose {
			mgr.RollbackPlanStart(oldMgr)
		}
		return err
	}
	_ = extension.TrackUIHub(rs.Scope(), gen)
	for _, client := range mgr.Clients() {
		_ = extension.TrackEventSubscription(rs.Scope(), "sidecar:"+client.PluginID(), func() error { return nil })
	}
	if res.Snapshot != nil {
		res.Snapshot = res.Snapshot.WithLiveContributions(gen, mgr.Contributions())
	}
	if err := stageDispatcher(res); err != nil {
		return err
	}
	base := res.BaseProviderResolver
	if base == nil {
		base = res.ProviderResolver
	}
	var claims map[extension.Slot]extension.ContributionSource
	if res.Snapshot != nil {
		claims = res.Snapshot.Replacements()
	}
	merged, merr := mergeSidecarProviders(base, mgr, claims, res.Owner)
	if merr != nil {
		return merr
	}
	if merged != nil {
		res.ProviderResolver = merged
	}
	return nil
}

func stageDispatcher(res *BuildResult) error {
	if res == nil || res.Snapshot == nil {
		return nil
	}
	clients := sidecarClientResolver(res.Extensions)
	required := requiredRuntimeSet(res.Extensions)
	res.Dispatcher = dispatch.New(res.Snapshot.InterceptorChain(), res.Snapshot.Replacements(), clients, required, dispatch.Options{})
	return nil
}

func commitControllerExtPatch(res *BuildResult, session protocol.SessionContext, gen uint64) error {
	if res == nil || res.Controller == nil {
		return nil
	}
	if res.Dispatcher != nil {
		res.Controller.ReplaceExtensions(res.Dispatcher)
	}
	if res.ProviderResolver != nil {
		res.Controller.SetProviderResolver(res.ProviderResolver)
		// Install stream routers only at commit so stage/ready failure never
		// leaves Unchanged clients pointing at a discarded generation resolver.
		installSidecarStreamRouters(res.Extensions, res.ProviderResolver)
	}
	if res.ExtensionUI != nil {
		if res.Extensions != nil {
			bindExtensionUI(res.ExtensionUI, res.Extensions, func(string) {})
		}
		res.ExtensionUI.BindGeneration(session.SessionID, gen)
		res.Controller.SetExtensionUI(res.ExtensionUI)
	}
	return nil
}

func awaitSidecarsReady(ctx context.Context, mgr *sidecar.Manager) error {
	if mgr == nil {
		return extension.AwaitReady(ctx, nil)
	}
	ready := make(chan struct{})
	go func() {
		_ = mgr.Clients()
		close(ready)
	}()
	return extension.AwaitReady(ctx, ready)
}

func controllerSessionID(c *control.Controller) string {
	if c == nil {
		return ""
	}
	if p := c.SessionPath(); p != "" {
		return p
	}
	return c.WorkspaceRoot()
}

func registerControllerDrainCancel(owner *extension.RuntimeOwner, gen uint64, ctrl *control.Controller) {
	if gen == 0 || ctrl == nil {
		return
	}
	if owner == nil {
		owner = extension.RuntimeOwnerOrDefault(nil)
	}
	owner.Gate.RegisterDrainCancel(gen, func() {
		if ctrl.RuntimeGeneration() == gen || ctrl.RuntimeGeneration() == 0 {
			ctrl.Cancel()
		}
	})
}

func finishRebuildPublish(res *BuildResult, drainMgr *sidecar.Manager, start time.Time) {
	publishBuildResult(res)
	if drainMgr != nil && res != nil && res.Plan != nil && !res.ReusedController {
		drainStart := time.Now()
		drainMgr.DrainPlan(res.Plan)
		extension.DefaultLifecycleMetrics.ObserveDrain(time.Since(drainStart))
	}
	if res != nil && res.Runtime != nil && res.Snapshot != nil {
		_ = res.Runtime.Track(extension.Effect{
			ID:    fmt.Sprintf("rebuild-publish-%d", res.Snapshot.Generation()),
			Owner: "boot",
			Class: extension.Irreversible,
			Dispose: func(context.Context) error {
				return nil
			},
		})
	}
	extension.DefaultLifecycleMetrics.ObserveActivate(time.Since(start))
}
