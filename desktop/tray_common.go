package main

func (a *App) trayLocale() string {
	cfg, _, err := a.loadDesktopUserConfigForView()
	if err != nil {
		return ""
	}
	return cfg.DesktopLanguage()
}

func (a *App) showFromTray() {
	a.showMainWindowFrom("tray")
}

func (a *App) quitFromTray() {
	a.quitApp()
}

type trayLabels struct {
	openTitle   string
	openTooltip string
	quitTitle   string
	quitTooltip string
}

func trayMenuLabels(locale string) trayLabels {
	if locale == "zh" {
		return trayLabels{
			openTitle:   "打开",
			openTooltip: "打开 Reasonix 窗口",
			quitTitle:   "退出",
			quitTooltip: "退出 Reasonix",
		}
	}
	return trayLabels{
		openTitle:   "Open",
		openTooltip: "Open the Reasonix window",
		quitTitle:   "Quit",
		quitTooltip: "Quit Reasonix",
	}
}
