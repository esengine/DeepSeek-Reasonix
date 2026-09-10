package main

import (
	"context"
	"log/slog"
	goruntime "runtime"
	"sync"
	"time"
)

const (
	desktopShellEvent = "desktop:shell-status"

	desktopDOMReadyTimeout      = 3 * time.Second
	desktopFrontendReadyTimeout = 15 * time.Second
)

type desktopShellPhase string

const (
	desktopShellStarting         desktopShellPhase = "starting"
	desktopShellDOMReady         desktopShellPhase = "dom_ready"
	desktopShellFrontendReady    desktopShellPhase = "frontend_ready"
	desktopShellVisible          desktopShellPhase = "visible"
	desktopShellBackgroundHidden desktopShellPhase = "background_hidden"
	desktopShellFailed           desktopShellPhase = "failed"
)

// desktopShellCoordinator is the single owner of main-window lifecycle state.
// Native window commands stay behind the nativeHost boundary, while every
// startup, tray, second-instance, menu and watchdog presentation goes through
// Present so platform ordering cannot drift again.
type desktopShellCoordinator struct {
	app *App

	mu               sync.Mutex
	phase            desktopShellPhase
	domReady         bool
	frontendReady    bool
	frontendFirstAt  time.Time
	healthy          bool
	presented        bool
	backgroundHidden bool
	watchdogCancel   context.CancelFunc
	presentOverride  func(string) // test-only, set before concurrent use
}

func newDesktopShellCoordinator(app *App) *desktopShellCoordinator {
	return &desktopShellCoordinator{app: app, phase: desktopShellStarting}
}

func (c *desktopShellCoordinator) start(ctx context.Context) {
	if c == nil || c.app == nil {
		return
	}
	c.mu.Lock()
	if c.watchdogCancel != nil {
		c.watchdogCancel()
	}
	watchdogCtx, cancel := context.WithCancel(ctx)
	c.watchdogCancel = cancel
	c.phase = desktopShellStarting
	c.mu.Unlock()

	c.app.goSafe("desktopDOMReadyWatchdog", func() {
		timer := time.NewTimer(desktopDOMReadyTimeout)
		defer timer.Stop()
		select {
		case <-watchdogCtx.Done():
			return
		case <-timer.C:
		}
		c.mu.Lock()
		ready := c.domReady
		c.mu.Unlock()
		if !ready {
			// A native surface is more useful than a StartHidden process with no
			// visible recovery path, even when the renderer is still starting.
			c.app.showMainWindowFrom("startup_dom_timeout")
		}
	})

	c.app.goSafe("desktopFrontendReadyWatchdog", func() {
		timer := time.NewTimer(desktopFrontendReadyTimeout)
		defer timer.Stop()
		select {
		case <-watchdogCtx.Done():
			return
		case <-timer.C:
		}
		c.mu.Lock()
		ready := c.frontendReady
		if !ready {
			c.phase = desktopShellFailed
		}
		c.mu.Unlock()
		if !ready {
			c.app.handleDesktopFrontendTimeout("startup")
		}
	})
}

func (c *desktopShellCoordinator) stop() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.watchdogCancel != nil {
		c.watchdogCancel()
		c.watchdogCancel = nil
	}
	c.mu.Unlock()
}

func (c *desktopShellCoordinator) markDOMReady() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.domReady = true
	if c.presented {
		c.phase = desktopShellVisible
	} else if !c.frontendReady {
		c.phase = desktopShellDOMReady
	}
	c.mu.Unlock()
}

// markFrontendHeartbeat separates the first React + host bridge frame from a
// stable renderer. Health requires a later heartbeat at least two seconds
// after the first, so one lucky bridge call cannot commit update/LKG state.
func (c *desktopShellCoordinator) markFrontendHeartbeat(now time.Time) (first, healthy bool) {
	if c == nil {
		return false, false
	}
	c.mu.Lock()
	first = !c.frontendReady
	c.frontendReady = true
	if first {
		c.frontendFirstAt = now
	}
	healthy = !c.healthy && !c.frontendFirstAt.IsZero() && now.Sub(c.frontendFirstAt) >= 2*time.Second
	if healthy {
		c.healthy = true
	}
	if c.backgroundHidden {
		c.phase = desktopShellBackgroundHidden
	} else if c.presented {
		c.phase = desktopShellVisible
	} else {
		c.phase = desktopShellFrontendReady
	}
	if c.watchdogCancel != nil {
		c.watchdogCancel()
		c.watchdogCancel = nil
	}
	c.mu.Unlock()
	return first, healthy
}

func (c *desktopShellCoordinator) Present(source string) {
	if c == nil || c.app == nil || c.app.ctx == nil {
		return
	}
	c.mu.Lock()
	wasMaximised := c.app.backgroundMaximised.Swap(false)
	applyDesktopPresentPlan(c.app.ctx, c.app.nativeHost(), desktopPresentPlanFor(goruntime.GOOS, wasMaximised))
	c.backgroundHidden = false
	c.presented = true
	c.phase = desktopShellVisible
	c.mu.Unlock()
	slog.Debug("desktop: present main window", "source", metricBucket(source), "platform", goruntime.GOOS)
}

// hideToBackground linearizes the final tray check with the hide transition.
// If the tray disappears immediately afterwards, trayStateChanged waits for
// this critical section and re-presents the now-hidden window.
func (c *desktopShellCoordinator) hideToBackground(ctx context.Context, canHide func() bool) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if canHide != nil && !canHide() {
		return false
	}
	c.backgroundHidden = true
	c.presented = false
	c.phase = desktopShellBackgroundHidden
	hideForBackground(ctx, c.app.nativeHost())
	return true
}

func (c *desktopShellCoordinator) trayStateChanged(ready bool) {
	if c == nil || ready {
		return
	}
	c.mu.Lock()
	hidden := c.backgroundHidden
	c.mu.Unlock()
	if hidden {
		if c.presentOverride != nil {
			c.presentOverride("tray_unavailable")
		} else {
			c.app.showMainWindowFrom("tray_unavailable")
		}
	}
}

type desktopPresentAction uint8

const (
	desktopPresentApplicationShow desktopPresentAction = iota + 1
	desktopPresentMaximise
	desktopPresentWindowShow
	desktopPresentUnminimise
)

// Electron's restore only unminimises a window; unlike the retired GTK
// gtk_window_present path it does not present a hidden Linux window. Every
// platform must explicitly show it, preserving maximised state when requested.
func desktopPresentPlanFor(goos string, wasMaximised bool) []desktopPresentAction {
	actions := make([]desktopPresentAction, 0, 3)
	if goos == "darwin" {
		actions = append(actions, desktopPresentApplicationShow)
	}
	if wasMaximised && goos != "darwin" {
		actions = append(actions, desktopPresentMaximise, desktopPresentWindowShow)
		return actions
	}
	return append(actions, desktopPresentWindowShow, desktopPresentUnminimise)
}

func applyDesktopPresentPlan(ctx context.Context, host nativeHost, actions []desktopPresentAction) {
	for _, action := range actions {
		switch action {
		case desktopPresentApplicationShow:
			host.ShowApplication(ctx)
		case desktopPresentMaximise:
			host.MaximiseWindow(ctx)
		case desktopPresentWindowShow:
			host.ShowWindow(ctx)
		case desktopPresentUnminimise:
			host.UnminimiseWindow(ctx)
		}
	}
}

// showMainWindowFrom presents the main window through the shell coordinator
// (or the raw present plan when no coordinator is attached, e.g. tests).
func (a *App) showMainWindowFrom(source string) {
	if a.ctx == nil {
		return
	}
	if a.desktopShell.coordinator != nil {
		a.desktopShell.coordinator.Present(source)
	} else {
		applyDesktopPresentPlan(a.ctx, a.nativeHost(), desktopPresentPlanFor("", a.backgroundMaximised.Swap(false)))
	}
	a.kickDeferredRebuildRetry()
}

// handleDesktopFrontendTimeout fires when the frontend heartbeat never
// arrived. The Electron shell reloads a crashed renderer itself; a page that
// is alive but never responsive still deserves a presented window and a
// diagnostics trail instead of a hidden process.
func (a *App) handleDesktopFrontendTimeout(source string) {
	if a == nil {
		return
	}
	slog.Warn("desktop: frontend never became ready", "source", metricBucket(source))
	a.recordDiagnosticMetric("desktop_frontend", "ready_timeout."+metricBucket(source))
	a.showMainWindowFrom("frontend_ready_timeout")
}
