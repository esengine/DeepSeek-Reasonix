package main

import "reasonix/internal/config"

// SetHideAmounts persists amount privacy without rebuilding controllers.
func (a *App) SetHideAmounts(hidden bool) error {
	return a.applyConfigOnly(func(c *config.Config) error {
		c.Desktop.HideAmounts = hidden
		return nil
	})
}

// SetStatusBarStyle updates the desktop status bar metric label style. UI-only,
// no rebuild needed.
func (a *App) SetStatusBarStyle(style string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopStatusBarStyle(style) })
}

// SetStatusBarItems updates the ordered visible desktop status bar items.
// UI-only, no rebuild needed.
func (a *App) SetStatusBarItems(items []string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopStatusBarItems(items) })
}
