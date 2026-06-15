//go:build darwin

package main

// systray creates the NSStatusItem (an NSWindow) on the calling goroutine, but
// Cocoa demands the main thread — which Wails owns — so the tray crashes the app
// at launch (#3223). macOS backgrounds via runtime.Hide + the Dock instead.
func traySupported() bool { return false }
