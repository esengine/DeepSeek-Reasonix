//go:build windows

package main

// traySupported returns true if the system supports tray functionality on Windows.
func traySupported() bool { return true }
