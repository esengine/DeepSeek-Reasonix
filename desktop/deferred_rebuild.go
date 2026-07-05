package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"reasonix/internal/agent"
)

// deferredRebuildRetryInterval is how often the retry loop probes a held
// session lease. Package-level so tests can shorten it.
var deferredRebuildRetryInterval = 2 * time.Second

// deferredRebuildState tracks tabs whose settings were saved to disk but whose
// runtime could not refresh because the session lease was held by another
// Reasonix process. A single background loop probes the lease and replays the
// rebuild once the other side releases it. The loop only runs after
// enableDeferredRebuildRetry (the wails startup hook); tests that never call
// it get the pending bookkeeping without a background goroutine.
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
	d := &a.deferredRebuild
	d.mu.Lock()
	if d.stopped || len(d.pending) == 0 {
		d.running = false
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
