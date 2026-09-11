package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"runtime"
	"sync"

	"reasonix/desktop/internal/hostrpc"
)

// hostShellBridge is present only when the Electron shell drives this
// process. It replaces the in-process tray and the native quit hooks with
// host/* requests and routes the shell's hostEvent notifications back into
// the App entry points the Wails callbacks used to call.
type hostShellBridge struct {
	app      *App
	server   *hostrpc.Server
	remoteMu sync.Mutex
	remote   *hostRemoteWindows
}

func (a *App) hostMode() bool { return a != nil && a.hostShell != nil }

type hostTrayLabels struct {
	OpenTitle   string `json:"openTitle"`
	OpenTooltip string `json:"openTooltip"`
	QuitTitle   string `json:"quitTitle"`
	QuitTooltip string `json:"quitTooltip"`
	Tooltip     string `json:"tooltip"`
}

type hostTrayResult struct {
	Ready  bool   `json:"ready"`
	Reason string `json:"reason"`
}

func (b *hostShellBridge) trayLabels(locale string) hostTrayLabels {
	labels := trayMenuLabels(locale)
	return hostTrayLabels{
		OpenTitle:   labels.openTitle,
		OpenTooltip: labels.openTooltip,
		QuitTitle:   labels.quitTitle,
		QuitTooltip: labels.quitTooltip,
		Tooltip:     "Reasonix",
	}
}

func (b *hostShellBridge) startTray() bool {
	a := b.app
	if a.shuttingDown.Load() || a.forceQuit.Load() {
		return false
	}
	a.mu.Lock()
	if a.tray != nil {
		a.mu.Unlock()
		return true
	}
	t := newDesktopTray()
	a.tray = t
	a.desktopShell.trayState = "probing"
	a.desktopShell.trayReason = ""
	a.trayReady = false
	a.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), rpcHostWindowTimeout)
	defer cancel()
	var result hostTrayResult
	err := b.server.Request(ctx, "host/tray.ensure", b.trayLabels(a.trayLocale()), &result)
	switch {
	case err != nil:
		a.setTrayHealth(t, "unavailable", "host_unreachable")
	case !result.Ready:
		reason := result.Reason
		if reason == "" {
			reason = "platform_no_tray"
		}
		a.setTrayHealth(t, "unavailable", reason)
	default:
		a.setTrayHealth(t, "ready", "")
	}
	if !result.Ready || err != nil {
		a.mu.Lock()
		if a.tray == t {
			a.tray = nil
		}
		a.mu.Unlock()
		return false
	}
	return true
}

func (b *hostShellBridge) stopTray() {
	a := b.app
	a.mu.Lock()
	t := a.tray
	a.tray = nil
	a.mu.Unlock()
	if t == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), rpcHostWindowTimeout)
	defer cancel()
	_ = b.server.Request(ctx, "host/tray.destroy", struct{}{}, nil)
	a.setTrayHealth(nil, "unavailable", "tray_exited")
}

func (b *hostShellBridge) updateTrayLocale(locale string) {
	a := b.app
	a.mu.RLock()
	present := a.tray != nil
	a.mu.RUnlock()
	if !present {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), rpcHostWindowTimeout)
	defer cancel()
	_ = b.server.Request(ctx, "host/tray.ensure", b.trayLabels(locale), nil)
}

// handleHostEvent maps shell-originated events onto the App entry points
// that Wails callbacks (second instance, tray menu, app menu) used to call.
func (b *hostShellBridge) handleHostEvent(_ context.Context, name string, payload json.RawMessage) error {
	a := b.app
	switch name {
	case "remoteWindow.closed":
		b.remoteWindowClosed(payload)
	case "secondInstance":
		a.goSafe("secondInstanceLaunch", a.secondInstanceLaunch)
	case "tray.open":
		a.goSafe("showFromTray", a.showFromTray)
	case "tray.quit":
		a.goSafe("quitFromTray", a.quitFromTray)
	case "menu.showWindow":
		a.goSafe("showMainWindow", a.showMainWindow)
	default:
		slog.Debug("desktop host: ignoring host event", "name", name)
	}
	return nil
}

// beforeClose translates the shell's close reason into the same signal the
// macOS Cmd+Q hook raised, so quit and updater exits bypass background close.
func (b *hostShellBridge) beforeClose(ctx context.Context, reason string) bool {
	if reason == "quit" || reason == "updater" {
		markSystemQuitRequested()
	}
	return b.app.beforeClose(ctx)
}

// startNativeShellSupport repairs the installed bundle's icon integration.
// Under the Electron shell the service owns no native UI thread, so the shell
// owns the window-level hooks; this runs only for a bare service launch.
func (a *App) startNativeShellSupport() {
	if a.hostMode() {
		return
	}
	a.goSafe("repairDesktopIconIntegration", func() {
		if err := repairDesktopIconIntegration(); err != nil {
			slog.Debug("desktop: repair native icon integration", "err", err)
		}
	})
}

// relaunch asks the shell to restart the whole application; the shell then
// drives beforeClose and shutdown, so the caller must not exit on its own.
func (b *hostShellBridge) relaunch() error {
	ctx, cancel := context.WithTimeout(context.Background(), rpcHostWindowTimeout)
	defer cancel()
	execPath := ""
	if runtime.GOOS != "darwin" {
		execPath = currentLauncherPath()
	}
	return b.server.Request(ctx, "host/app.relaunch", struct {
		Args     []string `json:"args"`
		ExecPath string   `json:"execPath,omitempty"`
	}{Args: []string{}, ExecPath: execPath}, nil)
}

// quit asks the shell to shut the application down without restarting it.
func (b *hostShellBridge) quit() error {
	ctx, cancel := context.WithTimeout(context.Background(), rpcHostWindowTimeout)
	defer cancel()
	return b.server.Request(ctx, "host/app.quit", struct{}{}, nil)
}

// relaunchDesktop restarts Reasonix after an update. Under the shell the
// restart is owned by Electron; the Wails build exits itself after handing
// off to the thin launcher.
func (a *App) relaunchDesktop(relaunchBinary bool) {
	if a.hostMode() {
		var err error
		if relaunchBinary {
			err = a.hostShell.relaunch()
		} else {
			err = a.hostShell.quit()
		}
		if err != nil {
			slog.Warn("desktop host: relaunch request failed", "err", err)
		}
		return
	}
	a.shutdown(a.ctx)
	if relaunchBinary {
		_ = relaunchThroughLauncher()
	}
	os.Exit(0)
}

// relaunchAfterPortableUpdate restarts Reasonix once the platform installer
// owns the swap. Under the shell on macOS the detached hand-off reopens the
// swapped bundle itself, so the shell must only quit.
func (a *App) relaunchAfterPortableUpdate() {
	if runtime.GOOS == "darwin" && a.hostMode() {
		if err := a.hostShell.quit(); err != nil {
			slog.Warn("desktop host: quit request failed", "err", err)
		}
		return
	}
	a.relaunchDesktop(runtime.GOOS == "linux")
}

// updateHandoffOwnerPID is the process the platform update helper waits for
// before it swaps the install: the Electron shell, which is the service's
// parent and holds the bundle open, under the shell; this process under Wails.
func (a *App) updateHandoffOwnerPID() int {
	if a.hostMode() {
		return os.Getppid()
	}
	return os.Getpid()
}
