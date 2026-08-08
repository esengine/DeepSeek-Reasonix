package boot

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/extension"
	"reasonix/internal/provider"
)

// RebuildFrom is Rebuild using previous BuildResult for incremental sidecars
// and subgraph-classified assembly (no-op / interceptor-only / UI-only, …).
func RebuildFrom(ctx context.Context, previous *BuildResult, opts Options) (*BuildResult, error) {
	if previous == nil || previous.Controller == nil {
		return nil, fmt.Errorf("boot: RebuildFrom requires the BuildResult being replaced")
	}
	if previous.Extensions != nil {
		opts.Extensions = previous.Extensions
	}
	if previous.Plan != nil && previous.Plan.Graph != nil {
		opts.Graph = previous.Plan.Graph
	}
	if previous.Snapshot != nil {
		opts.Generation = previous.Snapshot.Generation()
		opts.PreviousSnapshot = previous.Snapshot
	}
	if previous.Dispatcher != nil {
		opts.PreviousDispatcher = previous.Dispatcher
	}
	if previous.Owner != nil {
		opts.Owner = previous.Owner
	}
	return rebuildWithPrevious(ctx, previous.Controller, previous, opts)
}

// Rebuild builds a replacement runtime for old, migrating session state.
// On any failure the partially built runtime is closed and old keeps working.
//
// The caller passes the SAME SharedHost in opts.SharedHost that the old build
// used (when it used one), so the replacement reuses running MCP processes
// instead of respawning them per rebuild.
//
// Migrated state (all via public control APIs, mirroring the desktop settings
// rebuild and the CLI/ACP model switch):
//   - conversation history: old.History() resumes on the SAME session file
//     (agent.ContinueSessionPath), with the freshly composed system message
//     spliced over the outgoing one so the next turn speaks the rebuilt
//     profile contract;
//   - Goal and recovery sidecars: restored by the Resume inside AdoptHistory
//     whenever the session path persisted; when old never pinned a path (no
//     sidecar could exist), a running Goal is seeded from old's in-memory
//     state and the live recovery checkpoint is carried across;
//   - tool approval mode (Ask/Auto/Yolo) and the plan-mode flag — carried
//     faithfully, including the inconsistent plan+goal combination a legacy
//     session could hold, because Rebuild reproduces old's state rather than
//     re-interpreting it;
//   - same-session authorizations: "Allow for this session" grants and
//     Plan-mode read-only command trust (RestoreSessionAuthorizations);
//   - lifecycle markers (turn counter, started-once) via
//     InheritLifecycleFrom.
//
// Left to the frontend (Rebuild deliberately does not do these):
//   - swapping its controller pointer and closing old AFTER a successful
//     swap — old's controller and the old BuildResult.Runtime set stay the
//     caller's to release (CloseIfGeneration guards against closing a newer
//     runtime's resources);
//   - re-installing the interactive approval gate (EnableInteractiveApproval)
//     and re-binding approval/ask channels to the new controller;
//   - persisting the migrated transcript (Controller.Snapshot) when the swap
//     must be durable before it is published (ACP does this after migrating,
//     before publishing; desktop persists after the swap);
//   - session-lease coordination across the rebuild (desktop).
func Rebuild(ctx context.Context, old *control.Controller, opts Options) (*BuildResult, error) {
	return rebuildWithPrevious(ctx, old, nil, opts)
}

func rebuildWithPrevious(ctx context.Context, old *control.Controller, previous *BuildResult, opts Options) (*BuildResult, error) {
	if old == nil {
		return nil, fmt.Errorf("boot: Rebuild requires the controller being replaced")
	}
	if opts.Owner == nil {
		opts.Owner = old.RuntimeOwner()
	}
	// Capture migratable state before building: every accessor returns a
	// copy, so a slow build cannot observe a half-appended turn.
	m := runtimeMigration{
		prevPath:         old.SessionPath(),
		carried:          old.History(),
		authorizations:   old.SessionAuthorizations(),
		toolApprovalMode: old.ToolApprovalMode(),
		planMode:         old.PlanMode(),
		goal:             old.Goal(),
		goalRunning:      old.GoalStatus() == control.GoalStatusRunning,
	}
	// Reuse the previous Controller's session-private temporary directory so
	// model/settings hot rebuilds do not wipe temporary files mid-session.
	if opts.SessionTemp == nil {
		opts.SessionTemp = old.SessionTemp()
	}

	home := config.ReasonixHomeDir()
	// fromGraph must be the PREVIOUS generation's graph when available.
	// Building "current disk" for both from and to collapses every plan to no-op.
	var fromGraph *extension.DependencyGraph
	if previous != nil && previous.Plan != nil && previous.Plan.Graph != nil {
		fromGraph = previous.Plan.Graph
	} else if g, err := buildRuntimeGraph(home, nil); err == nil {
		fromGraph = g
	}
	opts.Graph = fromGraph

	// Prefer subgraph-classified rebuild when previous assembly is available.
	if previous != nil {
		if res, handled, err := tryRebuildSubgraph(ctx, old, previous, opts, m); handled {
			return res, err
		}
	}

	extension.DefaultLifecycleMetrics.FullRebuilds.Add(1)
	opts.deferPublish = true
	res, err := BuildRuntime(ctx, opts)
	if err != nil {
		// Activation failure: new generation never published; old keeps serving.
		return nil, err
	}

	var toGraph *extension.DependencyGraph
	if g, err := buildRuntimeGraph(home, nil); err == nil {
		toGraph = g
	}
	var previousSnapshot *extension.RuntimeSnapshot
	if previous != nil {
		previousSnapshot = previous.Snapshot
	}
	attachPlanAndStatus(res, fromGraph, toGraph, opts.Generation, previousSnapshot)

	if err := migrateRuntimeState(res.Controller, old, m); err != nil {
		// Fail-atomic: release the replacement; old keeps serving.
		// Activation never reached Active publish.
		if res.Snapshot != nil {
			res.Owner.Gate.BeginDrain(res.Snapshot.Generation())
		}
		res.Controller.ReleaseResources()
		if res.Runtime != nil {
			_ = res.Runtime.Close()
		}
		return nil, err
	}
	if prevGen := old.RuntimeGeneration(); prevGen != 0 && (res.Snapshot == nil || prevGen != res.Snapshot.Generation()) {
		registerControllerDrainCancel(res.Owner, prevGen, old)
		if host := old.Host(); host != nil {
			h := host
			res.Owner.Gate.RegisterDrainCancel(prevGen, func() { h.CancelInFlightMCP() })
		}
	}
	// Publish new generation only after Active + state migration. Then drain
	// Removed/Reloaded clients still held by the previous Manager.
	publishBuildResult(res)
	if opts.Extensions != nil && res.Plan != nil {
		opts.Extensions.DrainPlan(res.Plan)
	}
	// SessionEnd is not fired on ordinary rebuild.
	return res, nil
}

// runtimeMigration carries the captured old-controller state into
// migrateRuntimeState.
type runtimeMigration struct {
	prevPath         string
	carried          []provider.Message
	authorizations   control.SessionAuthorizations
	toolApprovalMode string
	planMode         bool
	goal             string
	goalRunning      bool
}

// migrateRuntimeState applies the captured state to the freshly built
// controller. Every step today is an infallible public control call; the
// error return is the fail-atomic seam for steps that gain failure modes.
func migrateRuntimeState(ctrl, old *control.Controller, m runtimeMigration) error {
	carried := spliceFreshSystemPrompt(m.carried, ctrl.History())
	path := agent.ContinueSessionPath(m.prevPath, ctrl.SessionDir(), ctrl.Label())
	ctrl.AdoptHistory(carried, path)

	// Re-apply session axes a rebuild must not reset.
	ctrl.SetToolApprovalMode(m.toolApprovalMode)
	ctrl.SetPlanMode(m.planMode)
	if m.goalRunning && strings.TrimSpace(m.goal) != "" && strings.TrimSpace(ctrl.Goal()) == "" {
		ctrl.SetGoal(m.goal)
	}
	if m.prevPath == "" {
		// No persisted recovery sidecar; carry the live checkpoint.
		ctrl.CarryRecoveryFrom(old)
	}

	ctrl.InheritLifecycleFrom(old)
	ctrl.RestoreSessionAuthorizations(m.authorizations)
	return nil
}

// spliceFreshSystemPrompt replaces the carried conversation's system message
// with the fresh build's, so the resumed session speaks the rebuilt profile
// contract. A carried conversation without a system message gets the fresh
// one prepended; a fresh build without one leaves the conversation untouched.
func spliceFreshSystemPrompt(carried, fresh []provider.Message) []provider.Message {
	var system *provider.Message
	for i := range fresh {
		if fresh[i].Role == provider.RoleSystem {
			system = &fresh[i]
			break
		}
	}
	if system == nil {
		return carried
	}
	out := append([]provider.Message(nil), carried...)
	for i := range out {
		if out[i].Role == provider.RoleSystem {
			out[i] = *system
			return out
		}
	}
	return append([]provider.Message{*system}, out...)
}
