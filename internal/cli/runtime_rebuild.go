package cli

import (
	tea "charm.land/bubbletea/v2"

	"reasonix/internal/i18n"
)

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
