package main

import (
	"context"
	"fmt"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"reasonix/internal/notify"
)

// smartCloseAction is the pure smart-close decision.
type smartCloseAction int

const (
	// smartCloseQuit allows the close: nothing is running.
	smartCloseQuit smartCloseAction = iota
	// smartCloseBackground hides to the tray: tasks are running and a restore
	// path exists.
	smartCloseBackground
	// smartCloseStayVisible prevents the close: tasks are running but no
	// tray/window restore path exists, so hiding would strand them.
	smartCloseStayVisible
)

// smartCloseDecision is the platform-independent smart-close policy, kept
// pure so every platform tests the same branches.
func smartCloseDecision(activeWork int, restoreAvailable bool) smartCloseAction {
	if activeWork == 0 {
		return smartCloseQuit
	}
	if !restoreAvailable {
		return smartCloseStayVisible
	}
	return smartCloseBackground
}

// smartClose implements the smart close-window behavior. The return value is
// Wails' OnBeforeClose contract: true prevents the close, false allows it.
//
//   - idle (no running turn, no pending input, no background jobs): quit for
//     real (false) without probing the tray at all;
//   - active work with a restore path: save tabs, hide to the tray with one
//     system notification (true);
//   - active work without a restore path: prevent the close (true) and keep
//     the window visible — a hidden process that can never be restored must
//     not be created, and active tasks must not be lost to an accidental
//     quit. The user quits explicitly via the menu.
func (a *App) smartClose(ctx context.Context) bool {
	count := a.activeRuntimeWorkCount()
	if count == 0 {
		return false // idle: allow the quit without probing the tray
	}
	switch smartCloseDecision(count, a.backgroundCloseHasRestorePath()) {
	case smartCloseStayVisible:
		// Prevent the close: keep the window visible and tell the user the
		// tasks are still running and to quit explicitly.
		a.notifyBackgroundedTasksStayVisible(count)
		return true
	default:
		// The Wails runtime calls require the lifecycle context; the App's
		// captured context is authoritative and is nil only in tests, where
		// the backgrounding side effects are skipped while the prevent-close
		// contract (return true) still holds.
		if a.ctx != nil {
			a.backgroundMaximised.Store(runtime.WindowIsMaximised(a.ctx))
			a.saveWindowStateSync()
			a.snapshotAllTabs()
			hideForBackground(a.ctx)
			a.updateTrayTaskCount(count)
		}
		a.notifyBackgroundedTasks(count)
		return true
	}
}

// activeRuntimeWorkCount counts the runtimes with active work across visible
// tabs and detached sessions.
func (a *App) activeRuntimeWorkCount() int {
	if a.runtimeWorkCounter != nil {
		return a.runtimeWorkCounter()
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	count := 0
	for _, tab := range a.tabs {
		if tab != nil && tab.Ctrl != nil && controllerHasActiveRuntimeWork(tab.Ctrl) {
			count++
		}
	}
	for _, tab := range a.detachedSessions {
		if tab != nil && tab.Ctrl != nil && controllerHasActiveRuntimeWork(tab.Ctrl) {
			count++
		}
	}
	return count
}

// notifyBackgroundedTasks sends the one-shot system notification when the
// window hides to the tray with active tasks.
func (a *App) notifyBackgroundedTasks(count int) {
	body := fmt.Sprintf("%d task(s) still running; Reasonix is in the tray", count)
	a.sendBackgroundTaskNotification(body)
}

// notifyBackgroundedTasksStayVisible notifies that tasks are still running
// and the window stayed visible because no tray restore path exists.
func (a *App) notifyBackgroundedTasksStayVisible(count int) {
	body := fmt.Sprintf("%d task(s) still running; the window stays open — use Quit Reasonix to exit", count)
	a.sendBackgroundTaskNotification(body)
}

func (a *App) sendBackgroundTaskNotification(body string) {
	sender := a.desktopNotificationSender()
	if sender == nil {
		return
	}
	_ = sender.Send(notify.Message{Title: "Reasonix", Body: body})
}

// updateTrayTaskCount reflects the active task count in the tray tooltip so a
// backgrounded Reasonix shows how much work is still running.
func (a *App) updateTrayTaskCount(count int) {
	a.mu.RLock()
	tray := a.tray
	a.mu.RUnlock()
	if tray == nil {
		return
	}
	tray.setTooltip(fmt.Sprintf("Reasonix — %d task(s) running", count))
}
