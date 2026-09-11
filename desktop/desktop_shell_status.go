package main

import goruntime "runtime"

type DesktopShellStatusView struct {
	TrayState                string `json:"trayState"`
	BackgroundCloseAvailable bool   `json:"backgroundCloseAvailable"`
	Reason                   string `json:"reason,omitempty"`
}

func (a *App) GetDesktopShellStatus() DesktopShellStatusView {
	if a == nil {
		return DesktopShellStatusView{TrayState: "unavailable", Reason: "shell_unavailable"}
	}
	a.mu.RLock()
	state := a.desktopShell.trayState
	reason := a.desktopShell.trayReason
	ready := a.trayReady
	a.mu.RUnlock()
	if state == "" {
		state = "probing"
	}
	return DesktopShellStatusView{
		TrayState:                state,
		BackgroundCloseAvailable: backgroundCloseUsesApplicationHide(goruntime.GOOS) || ready,
		Reason:                   reason,
	}
}

func (a *App) setTrayHealth(t *desktopTray, state, reason string) {
	if a == nil {
		return
	}
	if state != "probing" && state != "ready" && state != "unavailable" {
		state = "unavailable"
		reason = "invalid_state"
	}
	ready := state == "ready"
	a.mu.Lock()
	if t != nil && a.tray != t {
		a.mu.Unlock()
		return
	}
	changed := a.desktopShell.trayState != state || a.desktopShell.trayReason != reason || a.trayReady != ready
	a.desktopShell.trayState = state
	a.desktopShell.trayReason = reason
	a.trayReady = ready
	ctx := a.ctx
	a.mu.Unlock()

	if ready && t != nil {
		t.markReady()
	}
	if changed {
		bucket := reason
		if ready {
			bucket = "ready"
		}
		a.recordDiagnosticMetric("desktop_tray", metricBucket(bucket))
		if a.desktopShell.coordinator != nil && !a.shuttingDown.Load() && !a.forceQuit.Load() {
			a.desktopShell.coordinator.trayStateChanged(ready)
		}
		a.runtimeEvents.Emit(ctx, desktopShellEvent, a.GetDesktopShellStatus())
	}
}
