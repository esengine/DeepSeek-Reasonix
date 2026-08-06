package main

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"reasonix/internal/config"
)

// globalHotkeyHolder is the currently registered OS binding, if any.
type globalHotkeyHolder struct {
	binding string
	stop    context.CancelFunc
}

var (
	globalHotkeyMu     sync.Mutex
	globalHotkeyActive *globalHotkeyHolder
	// registerGlobalHotkeyBinding is replaced in tests.
	registerGlobalHotkeyBinding = platformRegisterGlobalHotkey
)

func (a *App) startGlobalHotkey() {
	if a == nil || a.remoteWindowTicket != "" {
		return
	}
	cfg, _, err := a.loadDesktopUserConfigForView()
	if err != nil {
		cfg = config.LoadForEdit(config.UserConfigPath())
	}
	if err := a.rebindGlobalHotkey(cfg.DesktopGlobalHotkey()); err != nil {
		slog.Warn("desktop: global hotkey unavailable", "binding", cfg.DesktopGlobalHotkey(), "err", err)
		a.emitGlobalHotkeyError(err)
	}
}

func (a *App) stopGlobalHotkey() {
	globalHotkeyMu.Lock()
	defer globalHotkeyMu.Unlock()
	if globalHotkeyActive != nil {
		globalHotkeyActive.stop()
		globalHotkeyActive = nil
	}
}

func (a *App) rebindGlobalHotkey(binding string) error {
	binding = strings.TrimSpace(binding)
	globalHotkeyMu.Lock()
	defer globalHotkeyMu.Unlock()
	if globalHotkeyActive != nil {
		globalHotkeyActive.stop()
		globalHotkeyActive = nil
	}
	if binding == "" {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	onTrigger := func() {
		a.goSafe("toggleFromHotkey", a.toggleMainWindowFromHotkey)
	}
	if err := registerGlobalHotkeyBinding(ctx, binding, onTrigger); err != nil {
		cancel()
		return err
	}
	globalHotkeyActive = &globalHotkeyHolder{binding: binding, stop: cancel}
	return nil
}

func (a *App) emitGlobalHotkeyError(err error) {
	if a == nil || a.ctx == nil || err == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "desktop:global-hotkey-error", map[string]string{
		"message": err.Error(),
	})
}

// toggleMainWindowFromHotkey implements Spotlight-style summon: raise when the
// window is hidden/minimized/behind, hide when Reasonix is already frontmost.
func (a *App) toggleMainWindowFromHotkey() {
	if a == nil || a.ctx == nil {
		return
	}
	if a.shouldShowOnGlobalHotkey() {
		a.showMainWindowFrom("hotkey")
		return
	}
	a.backgroundMaximised.Store(a.lastKnownMaximised())
	a.backgroundHidden.Store(true)
	a.saveWindowStateSync()
	hideForBackground(a.ctx)
}

func (a *App) shouldShowOnGlobalHotkey() bool {
	if a == nil {
		return true
	}
	if a.backgroundHidden.Load() {
		return true
	}
	if a.ctx != nil && windowLooksBackgrounded(a.ctx) {
		return true
	}
	return !windowIsFrontmost()
}
