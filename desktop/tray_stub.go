//go:build !windows

package main

import "context"

// trayStub provides empty no-op implementations for non-Windows platforms.
// The system tray is a Windows-only feature; on macOS/Linux the app window
// is simply shown/hidden through the standard desktop shell (Dock / taskbar).

type trayMgr struct{}

func newTrayMgr(_ context.Context) *trayMgr { return &trayMgr{} }

func (t *trayMgr) start() error { return nil }

func (t *trayMgr) stop() {}
