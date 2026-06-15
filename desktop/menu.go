package main

import (
	goruntime "runtime"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// createAppMenu builds the native application menu bar. macOS only: it's the
// platform convention there, and the Edit menu's standard roles are what make
// Cmd+C/V work in the webview. On Windows/Linux a menu bar renders as a stray
// in-window "File" strip (the Edit/Window mac roles don't show), so return nil.
func (a *App) createAppMenu() *menu.Menu {
	if goruntime.GOOS != "darwin" {
		return nil
	}

	m := menu.NewMenu()

	m.Append(menu.AppMenu())

	fileMenu := m.AddSubmenu("File")
	fileMenu.AddText("Settings", keys.CmdOrCtrl(","), func(_ *menu.CallbackData) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "app:open-settings")
		}
	})
	fileMenu.AddText("Toggle Developer Tools", keys.CmdOrCtrl("i"), func(_ *menu.CallbackData) {
		if a.ctx != nil {
			runtime.WindowExecJS(a.ctx, `window.webkit.messageHandlers.external.postMessage("wails:openInspector");`)
		}
	})
	fileMenu.AddText("Show Reasonix", nil, func(_ *menu.CallbackData) {
		a.showMainWindow()
	})
	fileMenu.AddText("Quit Reasonix", keys.CmdOrCtrl("q"), func(_ *menu.CallbackData) {
		a.quitApp()
	})
	m.Append(menu.EditMenu())
	m.Append(menu.WindowMenu())

	return m
}
