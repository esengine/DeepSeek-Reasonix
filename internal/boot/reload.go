package boot

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/provider"
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
	if old == nil {
		return nil, fmt.Errorf("boot: Rebuild requires the controller being replaced")
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
	res, err := BuildRuntime(ctx, opts)
	if err != nil {
		return nil, err
	}
	if err := migrateRuntimeState(res.Controller, old, m); err != nil {
		// Fail-atomic: nothing was published, so release the replacement
		// without firing SessionEnd (the session logically continues on old)
		// and close its runtime set — empty in stage 3a — so stage-5
		// resources can never leak through this path. old is never closed
		// here; its runtime set stays the caller's to release after a
		// successful swap.
		res.Controller.ReleaseResources()
		if res.Runtime != nil {
			_ = res.Runtime.Close()
		}
		return nil, err
	}
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
// error return is the fail-atomic seam for steps that gain failure modes
// (for example persisting the migrated transcript), so Rebuild's cleanup
// path is real rather than assumed.
func migrateRuntimeState(ctrl, old *control.Controller, m runtimeMigration) error {
	carried := spliceFreshSystemPrompt(m.carried, ctrl.History())
	path := agent.ContinueSessionPath(m.prevPath, ctrl.SessionDir(), ctrl.Label())
	ctrl.AdoptHistory(carried, path)

	// Re-apply the session axes a rebuild must not reset (mirrors the ACP
	// session-config switch). The Goal sidecar restored by the Resume above
	// is authoritative; the in-memory Goal is seeded only when nothing was
	// restored (the outgoing controller never pinned a session path).
	ctrl.SetToolApprovalMode(m.toolApprovalMode)
	ctrl.SetPlanMode(m.planMode)
	if m.goalRunning && strings.TrimSpace(m.goal) != "" && strings.TrimSpace(ctrl.Goal()) == "" {
		ctrl.SetGoal(m.goal)
	}
	if m.prevPath == "" {
		// No persisted recovery sidecar could have been restored, so carry
		// the live in-memory checkpoint across the boundary.
		ctrl.CarryRecoveryFrom(old)
	}

	// Same-session lifecycle and grants: the replacement keeps the turn
	// counter / started-once flag and every "Allow for this session" and
	// Plan-mode read-only trust grant the user already made.
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
