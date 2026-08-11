package cli

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/boot"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
)

// runtimeRebuilder builds the /reload replacement controller through
// boot.Rebuild with the session's current model/profile/effort. cli.go
// supplies it because that is where the boot.Options (and the SharedHost, when
// one exists) live; the TUI owns only the queue/swap/close sequencing.
type runtimeRebuilder func(ctx context.Context, spec controllerBuildSpec, old *control.Controller) (*boot.BuildResult, error)

// reloadDisposition is the /reload decision, extracted so the queue semantics
// are unit-testable without a running tea program.
type reloadDisposition int

const (
	// reloadUnavailable means this session has no rebuild seam (headless or
	// test surface): /reload can only report unavailable.
	reloadUnavailable reloadDisposition = iota
	// reloadNow means the TUI is idle: rebuild immediately.
	reloadNow
	// reloadQueued means a turn or a runtime switch is in flight: coalesce
	// exactly one reload and run it from the idle drain.
	reloadQueued
)

func (m *chatTUI) reloadDisposition() reloadDisposition {
	if m == nil || m.ctrl == nil || m.rebuildRuntime == nil {
		return reloadUnavailable
	}
	if m.modelSwitchPending || m.runtimeSwitchBusy() {
		return reloadQueued
	}
	return reloadNow
}

// runReloadCommand handles "/reload": rebuild the agent runtime (tools,
// skills, commands, hooks, MCP servers, providers) in place, keeping this
// session. A turn or rebuild in flight queues exactly one reload; the
// TurnDone drain runs it once the TUI is idle.
func (m *chatTUI) runReloadCommand() tea.Cmd {
	if m == nil || m.ctrl == nil {
		return nil
	}
	switch m.reloadDisposition() {
	case reloadUnavailable:
		m.notice(i18n.M.RuntimeRefreshUnavailable)
	case reloadQueued:
		// Coalesce: repeated /reload while busy keeps a single queued reload.
		if !m.pendingReload {
			m.pendingReload = true
			m.notice(i18n.M.RuntimeReloadQueued)
		}
	case reloadNow:
		return m.scheduleRuntimeReload()
	}
	return nil
}

// drainQueuedRuntimeReload runs a /reload queued while the TUI was busy. It is
// called from the TurnDone drain and after a runtime switch settles; the busy
// re-check keeps the reload queued when background work outlives the turn.
func (m *chatTUI) drainQueuedRuntimeReload() tea.Cmd {
	if m == nil || !m.pendingReload {
		return nil
	}
	if m.reloadDisposition() != reloadNow {
		return nil
	}
	return m.scheduleRuntimeReload()
}

// scheduleRuntimeReload rebuilds the current controller through boot.Rebuild
// (same model/profile/effort; fresh extension discovery and migrated session
// state) and swaps it in only after the replacement is fully built. The swap
// and the old controller's retirement land in the shared modelSwitchMsg
// handler, exactly like a model switch: on failure the old controller keeps
// serving; on success the old one is closed at exit (closing it inside the
// TUI corrupts bubbletea's raw mode — see oldControllers).
func (m *chatTUI) scheduleRuntimeReload() tea.Cmd {
	if m == nil || m.ctrl == nil || m.rebuildRuntime == nil {
		return nil
	}
	oldCtrl, ok := m.ctrl.(*control.Controller)
	if !ok {
		// The real TUI always drives a *control.Controller; a SessionAPI test
		// double has no boot.Options to rebuild from.
		m.notice(i18n.M.RuntimeRefreshUnavailable)
		return nil
	}
	m.pendingReload = false
	if err := m.ctrl.Snapshot(); err != nil {
		m.notice("reload: snapshot failed: " + err.Error())
	}
	// Move the lease to the controller's current file before the replacement
	// binds it for writing (mirrors scheduleCurrentControllerRebuild). Capture
	// the path after Snapshot: a conflict retarget can move it.
	resumePath := m.ctrl.SessionPath()
	if err := m.rebindSessionLease(resumePath); err != nil {
		m.notice("reload: " + sessionLeaseHeldNotice(err))
		return nil
	}

	rebuild := m.rebuildRuntime
	spec := controllerBuildSpec{
		ModelRef:       m.modelRef,
		RuntimeProfile: m.runtimeProfile,
		// EffortOverride stays nil: the captured build options in cli.go
		// already track the session's current effort.
	}
	m.modelSwitchPending = true
	m.pendingModelSwitch = func() tea.Msg {
		res, err := rebuild(context.Background(), spec, oldCtrl)
		if err != nil {
			return modelSwitchMsg{ref: spec.ModelRef, failurePrefix: "reload", err: err}
		}
		c := res.Controller
		notice := i18n.M.RuntimeReloaded
		if res.Snapshot != nil {
			notice = fmt.Sprintf(i18n.M.RuntimeReloadedGenerationFmt, res.Snapshot.Generation())
		}
		// res.Runtime is the stage-3a (always empty) runtime set; when stage 5
		// binds sidecar processes it must retire alongside the controller via
		// oldControllers.
		return modelSwitchMsg{
			ref:           spec.ModelRef,
			ctrl:          c,
			oldCtrl:       oldCtrl,
			label:         c.Label(),
			commands:      c.Commands(),
			skills:        c.SlashSkills(),
			host:          c.Host(),
			successNotice: notice,
		}
	}
	return m.pendingModelSwitch
}

// runtimeSettingChangeReady guards settings that need a controller rebuild.
// A nil controller is a non-interactive/test surface where persistence can
// still proceed without an in-session refresh.
func (m *chatTUI) runtimeSettingChangeReady() bool {
	if m == nil || m.ctrl == nil {
		return true
	}
	if m.buildController == nil {
		m.notice(i18n.M.RuntimeRefreshUnavailable)
		return false
	}
	if m.runtimeSwitchBusy() {
		m.notice(i18n.M.RuntimeRefreshBusy)
		return false
	}
	if m.modelSwitchPending {
		m.notice(i18n.M.RuntimeSwitchPending)
		return false
	}
	return true
}

// scheduleCurrentControllerRebuild refreshes configuration-backed runtime state
// without changing the active model/profile. The old controller remains usable
// until a fully initialized replacement is ready.
func (m *chatTUI) scheduleCurrentControllerRebuild(reason, successNotice string) tea.Cmd {
	if m == nil || m.ctrl == nil {
		return nil
	}
	if m.buildController == nil {
		m.notice(i18n.M.RuntimeRefreshUnavailable)
		return nil
	}
	if err := m.ctrl.Snapshot(); err != nil {
		m.notice(reason + ": snapshot failed: " + err.Error())
	}
	carried := m.ctrl.History()
	resumePath := m.ctrl.SessionPath()
	if err := m.rebindSessionLease(resumePath); err != nil {
		m.notice(reason + ": " + sessionLeaseHeldNotice(err))
		return nil
	}

	oldCtrl := m.ctrl
	build := m.buildController
	ref := m.modelRef
	profile := m.runtimeProfile
	m.modelSwitchPending = true
	m.pendingModelSwitch = func() tea.Msg {
		c, err := build(controllerBuildSpec{
			ModelRef:         ref,
			RuntimeProfile:   profile,
			ToolApprovalMode: oldCtrl.ToolApprovalMode(),
			PlanMode:         oldCtrl.PlanMode(),
		}, carried, resumePath, oldCtrl)
		if err != nil {
			return modelSwitchMsg{ref: ref, failurePrefix: reason, err: err}
		}
		return modelSwitchMsg{
			ref:           ref,
			ctrl:          c,
			oldCtrl:       oldCtrl,
			label:         c.Label(),
			commands:      c.Commands(),
			skills:        c.SlashSkills(),
			host:          c.Host(),
			successNotice: successNotice,
		}
	}
	return m.pendingModelSwitch
}

func (m *chatTUI) bindRuntimeRebuilder(maxSteps int, sink event.Sink, yolo bool, overrides cliBuildOverrides, buildOpts func(string, int, bool, event.Sink, string, cliBuildOverrides) boot.Options) {
	m.rebuildRuntime = func(ctx context.Context, spec controllerBuildSpec, old *control.Controller) (*boot.BuildResult, error) {
		effectiveOverrides := overrides
		if spec.EffortOverride != nil {
			effectiveOverrides.Effort = spec.EffortOverride
		}
		opts := buildOpts(spec.ModelRef, maxSteps, false, sink, spec.RuntimeProfile, effectiveOverrides)
		var res *boot.BuildResult
		var err error
		if m.lastBuildResult != nil {
			res, err = boot.RebuildFrom(ctx, m.lastBuildResult, opts)
		} else {
			res, err = boot.Rebuild(ctx, old, opts)
		}
		if err != nil {
			return nil, err
		}
		m.lastBuildResult = res
		res.Controller.EnableInteractiveApproval()
		if yolo {
			res.Controller.SetAutoApproveTools(true)
		}
		return res, nil
	}
}
