package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
)

// deferredRebuildRetryInterval is how often the retry loop probes a held
// session lease. Package-level so tests can shorten it.
var deferredRebuildRetryInterval = 2 * time.Second

const deferredStartupBuildLabel = "__startup__"

// deferredRebuildState tracks tabs whose settings were saved to disk but whose
// runtime could not refresh, plus tabs whose initial startup failed, because
// the session lease was held by another Reasonix process. A single background
// loop probes the lease and replays the rebuild once the other side releases
// it. The loop only runs after enableDeferredRebuildRetry (the wails startup
// hook); tests that never call it get the pending bookkeeping without a
// background goroutine.
type deferredRebuildState struct {
	mu      sync.Mutex
	pending map[string]string // tab ID -> setting label for notices
	enabled bool
	running bool
	stopped bool
	stop    chan struct{}
}

// enableDeferredRebuildRetry arms the retry loop; called from startup.
func (a *App) enableDeferredRebuildRetry() {
	d := &a.deferredRebuild
	d.mu.Lock()
	defer d.mu.Unlock()
	d.enabled = true
	a.startDeferredRebuildLoopLocked()
}

// startDeferredRebuildLoopLocked starts the loop when it is armed, idle, and
// has work. Callers must hold d.mu.
func (a *App) startDeferredRebuildLoopLocked() {
	d := &a.deferredRebuild
	if !d.enabled || d.running || d.stopped || len(d.pending) == 0 {
		return
	}
	d.running = true
	if d.stop == nil {
		d.stop = make(chan struct{})
	}
	go a.deferredRebuildLoop(d.stop)
}

// scheduleDeferredRebuild records that tabID needs a runtime refresh for
// setting and starts the retry loop if it is not running yet. Repeated calls
// for the same tab collapse into one retry carrying the latest label.
func (a *App) scheduleDeferredRebuild(tabID, setting string) {
	tabID = strings.TrimSpace(tabID)
	if tabID == "" {
		return
	}
	d := &a.deferredRebuild
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return
	}
	if d.pending == nil {
		d.pending = map[string]string{}
	}
	d.pending[tabID] = setting
	a.startDeferredRebuildLoopLocked()
}

func (a *App) scheduleDeferredStartupBuild(tabID string) {
	a.scheduleDeferredRebuild(tabID, deferredStartupBuildLabel)
}

func isDeferredStartupBuild(setting string) bool {
	return setting == deferredStartupBuildLabel
}

func (a *App) clearDeferredRebuild(tabID string) {
	d := &a.deferredRebuild
	d.mu.Lock()
	delete(d.pending, tabID)
	d.mu.Unlock()
}

func (a *App) deferredRebuildPending(tabID string) bool {
	d := &a.deferredRebuild
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.pending[tabID]
	return ok
}

// stopDeferredRebuildRetry permanently stops the retry loop; used on shutdown
// and by tests.
func (a *App) stopDeferredRebuildRetry() {
	d := &a.deferredRebuild
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return
	}
	d.stopped = true
	if d.stop != nil {
		close(d.stop)
	}
}

func (a *App) deferredRebuildLoop(stop <-chan struct{}) {
	ticker := time.NewTicker(deferredRebuildRetryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			d := &a.deferredRebuild
			d.mu.Lock()
			d.running = false
			d.mu.Unlock()
			return
		case <-ticker.C:
		}
		if a.deferredRebuildTickDone() {
			return
		}
	}
}

// deferredRebuildTickDone runs one retry pass and reports true when the loop
// should exit because nothing is pending anymore.
func (a *App) deferredRebuildTickDone() bool {
	return a.deferredRebuildTick(true)
}

func (a *App) deferredRebuildTick(markIdle bool) bool {
	d := &a.deferredRebuild
	d.mu.Lock()
	if d.stopped || len(d.pending) == 0 {
		if markIdle {
			d.running = false
		}
		d.mu.Unlock()
		return true
	}
	pending := make(map[string]string, len(d.pending))
	for id, setting := range d.pending {
		pending[id] = setting
	}
	d.mu.Unlock()

	for tabID, setting := range pending {
		a.retryDeferredRebuild(tabID, setting)
	}
	return false
}

func (a *App) kickDeferredRebuildRetry() {
	if a.ctx == nil {
		return
	}
	a.goSafe("deferredRebuildKick", func() {
		_ = a.deferredRebuildTick(false)
	})
}

func (a *App) retryDeferredRebuild(tabID, setting string) {
	if a.ctx == nil {
		return
	}
	tab := a.tabByID(tabID)
	if tab == nil || tab.ID != tabID {
		// The tab is gone; nothing left to refresh.
		a.clearDeferredRebuild(tabID)
		return
	}
	if isDeferredStartupBuild(setting) {
		a.retryDeferredStartupBuild(tabID, tab)
		return
	}
	// Hold the rebuild mutex across probe + rebuild: the probe briefly acquires
	// the session lease, and a concurrent manual rebuild's ensureSessionLease
	// would see that probe as "held by another runtime" and spuriously defer.
	a.runtimeRebuildMu.Lock()
	defer a.runtimeRebuildMu.Unlock()
	// rebuildSettingLocked refreshes the active tab only. Wait until the user
	// is back on this tab so we refresh the runtime the pending setting was
	// meant for, not whichever tab happens to be focused.
	if a.activeTab() != tab {
		return
	}
	ctrl := a.controllerForTab(tab)
	if ctrl == nil {
		// Mid-(re)build on another path (provider retarget, workspace repair);
		// racing a second build+swap against it is what this loop must avoid.
		return
	}
	if controllerHasActiveRuntimeWork(ctrl) {
		return
	}
	if !a.deferredRebuildLeaseLooksFree(tab) {
		return
	}
	err := a.rebuildSettingLocked(setting)
	if err == nil {
		// rebuildSettingLocked already cleared the pending entry for the tab it
		// refreshed; just announce it.
		a.noticeForTab(tabID, fmt.Sprintf("%s applied: session refreshed after the lease was released", setting))
		return
	}
	if errors.Is(err, agent.ErrSessionLeaseHeld) {
		return // grabbed back before we could rebuild; keep waiting
	}
	var busy *rebuildBusyError
	if errors.As(err, &busy) {
		return // a turn started meanwhile; retry once it finishes
	}
	// Anything else will not resolve by waiting; give up loudly instead of
	// retrying forever.
	a.clearDeferredRebuild(tabID)
	slog.Warn("desktop: deferred settings rebuild failed", "setting", setting, "tab", tabID, "err", err)
	a.warnForTab(tabID, fmt.Sprintf("%s was saved but the session could not refresh: %s", setting, err.Error()))
}

func (a *App) retryDeferredStartupBuild(tabID string, tab *WorkspaceTab) {
	a.runtimeRebuildMu.Lock()
	defer a.runtimeRebuildMu.Unlock()
	if !a.tabHasRetryableStartupLeaseError(tab) {
		a.clearDeferredRebuild(tabID)
		return
	}
	if !a.deferredRebuildLeaseLooksFree(tab) {
		return
	}
	err := a.rebuildStartupTabLocked(tab)
	if err == nil {
		a.clearDeferredRebuild(tabID)
		return
	}
	if errors.Is(err, agent.ErrSessionLeaseHeld) {
		return
	}
	a.clearDeferredRebuild(tabID)
	slog.Warn("desktop: deferred session startup failed", "tab", tabID, "err", err)
}

func (a *App) tabHasRetryableStartupLeaseError(tab *WorkspaceTab) bool {
	if tab == nil {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.tabs[tab.ID] == tab && !tab.removed && tab.Ctrl == nil && tab.StartupErrLeaseHeld
}

func (a *App) rebuildStartupTabLocked(tab *WorkspaceTab) error {
	buildCtx, cancel := context.WithCancel(a.bootContext())
	a.mu.Lock()
	if tab == nil || a.tabs[tab.ID] != tab || tab.removed {
		a.mu.Unlock()
		cancel()
		return nil
	}
	if tab.Ctrl != nil {
		a.mu.Unlock()
		cancel()
		return nil
	}
	if !tab.StartupErrLeaseHeld {
		a.mu.Unlock()
		cancel()
		return nil
	}
	tab.buildGeneration++
	generation := tab.buildGeneration
	if tab.buildCancel != nil {
		tab.buildCancel()
	}
	tab.buildCancel = cancel
	tab.Ready = false
	clearTabStartupError(tab)
	tab.ActivityStatus = ""
	if tab.sink == nil {
		tab.sink = &tabEventSink{tabID: tab.ID, app: a, ctx: a.ctx}
	}
	a.saveTabsLocked()
	a.mu.Unlock()

	a.buildTabControllerWithContext(tab, loadedTabSession{}, buildCtx, generation, cancel)

	a.mu.RLock()
	stillCurrent := false
	var ctrl control.SessionAPI
	startupErr := ""
	leaseHeld := false
	if tab != nil {
		stillCurrent = a.tabs[tab.ID] == tab && !tab.removed
		ctrl = tab.Ctrl
		startupErr = tab.StartupErr
		leaseHeld = tab.StartupErrLeaseHeld
	}
	a.mu.RUnlock()
	if !stillCurrent || ctrl != nil {
		return nil
	}
	if leaseHeld {
		return agent.ErrSessionLeaseHeld
	}
	if strings.TrimSpace(startupErr) != "" {
		return fmt.Errorf("session startup: %s", startupErr)
	}
	return fmt.Errorf("session startup: controller was not built")
}

func (a *App) tryRecoverStartupLeaseHeldTab(tab *WorkspaceTab) bool {
	if a.ctx == nil || !a.tabHasRetryableStartupLeaseError(tab) {
		return false
	}
	a.runtimeRebuildMu.Lock()
	defer a.runtimeRebuildMu.Unlock()
	if !a.tabHasRetryableStartupLeaseError(tab) {
		return a.controllerForTab(tab) != nil
	}
	if !a.deferredRebuildLeaseLooksFree(tab) {
		return false
	}
	err := a.rebuildStartupTabLocked(tab)
	if err == nil {
		a.clearDeferredRebuild(tab.ID)
		return a.controllerForTab(tab) != nil
	}
	if errors.Is(err, agent.ErrSessionLeaseHeld) {
		a.scheduleDeferredStartupBuild(tab.ID)
	} else {
		a.clearDeferredRebuild(tab.ID)
	}
	return false
}

// deferredRebuildLeaseLooksFree cheaply probes whether the tab's session lease
// could be acquired right now, without touching tab.sessionLease (only the
// serialized rebuild paths may mutate that). The probe path can lag the
// reconciled path the rebuild will use; a stale answer either re-defers on the
// next tick or lets the rebuild fail back into the pending set, so a mismatch
// only delays the retry.
func (a *App) deferredRebuildLeaseLooksFree(tab *WorkspaceTab) bool {
	a.mu.RLock()
	ctrl := tab.Ctrl
	path := strings.TrimSpace(tab.SessionPath)
	a.mu.RUnlock()
	if ctrl != nil {
		if p := strings.TrimSpace(ctrl.SessionPath()); p != "" {
			path = p
		}
	}
	if path == "" {
		return true // nothing to probe; let the rebuild decide
	}
	lease, err := agent.TryAcquireSessionLease(sessionRuntimeKey(path))
	if err != nil {
		var leaseErr *agent.SessionLeaseError
		if errors.As(err, &leaseErr) && leaseErr.Info != nil && leaseErr.Info.PID == os.Getpid() {
			if host, _ := os.Hostname(); leaseErr.Info.Hostname == host {
				// This process (usually this very tab) holds the probed path;
				// the blocking lease is some other path. Attempt the rebuild
				// and let its own lease checks decide.
				return true
			}
		}
		return !errors.Is(err, agent.ErrSessionLeaseHeld)
	}
	lease.Release()
	return true
}
