package boot

import (
	"context"
	"errors"
	"fmt"
	"reasonix/internal/state/sessionstore"
	"strings"

	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/extension"
	"reasonix/internal/session/control"
)

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
//     (sessionstore.ContinueSessionPath), with the freshly composed system message
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
	if _, observed := old.ObservePosture(); observed && opts.Observe == nil {
		return nil, ErrObserveRebuild
	}
	if opts.Owner == nil {
		opts.Owner = old.RuntimeOwner()
	}
	m := CaptureRuntimeMigration(old)
	// Reuse the previous Controller's session-private temporary directory so
	// model/settings hot rebuilds do not wipe temporary files mid-session.
	if opts.SessionTemp == nil {
		opts.SessionTemp = old.SessionTemp()
	}
	if opts.BrowserSession == nil {
		opts.BrowserSession = old.BrowserSession()
	}
	if opts.WorkspaceRepo.Dir == "" {
		opts.WorkspaceRepo = old.WorkspaceRepo()
	}

	home := opts.roots().Home()
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
	if previous != nil && opts.Observe == nil && !opts.ForceFullRebuild && !changesModel(opts, old) {
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

	if err := ApplyRuntimeMigration(res.Controller, old, m); err != nil {
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

// RuntimeMigration is the live state a rebuilt runtime carries over: the
// conversation, and the session axes a switch must not silently reset.
type RuntimeMigration struct {
	prevPath         string
	carried          []provider.Message
	authorizations   control.SessionAuthorizations
	toolApprovalMode string
	postureDefaulted bool
	planMode         bool
	goal             string
	goalRunning      bool
	modelMode        string
}

// CaptureRuntimeMigration reads the outgoing runtime's state. Capture it
// before building the replacement: every accessor returns a copy, so a slow
// build cannot observe a half-appended turn.
func CaptureRuntimeMigration(old control.SessionAPI) RuntimeMigration {
	m := RuntimeMigration{
		prevPath:         old.SessionPath(),
		carried:          old.History(),
		toolApprovalMode: old.ToolApprovalMode(),
		postureDefaulted: old.Posture().Defaulted,
		planMode:         old.PlanMode(),
		goal:             old.Goal(),
		goalRunning:      old.GoalStatus() == control.GoalStatusRunning,
	}
	if ctrl, ok := old.(*control.Controller); ok {
		m.authorizations = ctrl.SessionAuthorizations()
		m.modelMode = ctrl.ModelMode()
	}
	return m
}

// ApplyRuntimeMigration is the single definition of what survives a rebuild,
// so a frontend assembling its own replacement carries exactly what boot does.
// old may be nil (outgoing runtime is not a live Controller); the two steps
// reading it are skipped. Steps are infallible public control calls today —
// the error return is the fail-atomic seam for ones that gain failure modes.
func ApplyRuntimeMigration(ctrl, old *control.Controller, m RuntimeMigration) error {
	carried := spliceFreshSystemPrompt(m.carried, ctrl.History())
	path := sessionstore.ContinueSessionPath(m.prevPath, ctrl.SessionDir(), ctrl.Label())
	ctrl.AdoptHistory(carried, path)

	// Re-apply session axes a rebuild must not reset.
	if m.postureDefaulted {
		ctrl.ApplyDefaultPosture()
	} else {
		ctrl.SetToolApprovalMode(m.toolApprovalMode)
	}
	ctrl.SetPlanMode(m.planMode)
	if m.goalRunning && strings.TrimSpace(m.goal) != "" && strings.TrimSpace(ctrl.Goal()) == "" {
		ctrl.SetGoal(m.goal)
	}
	if old != nil {
		if m.prevPath == "" {
			// No persisted recovery sidecar; carry the live checkpoint.
			ctrl.CarryRecoveryFrom(old)
		}
		ctrl.InheritLifecycleFrom(old)
	}
	ctrl.RestoreSessionAuthorizations(m.authorizations)
	// A mode survives only onto a model that declares it: switching to one
	// that does not is what turns it off, rather than a request it would 400.
	if err := ctrl.SetModelMode(m.modelMode); err != nil && !errors.Is(err, control.ErrModelModeUnsupported) {
		return err
	}
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
