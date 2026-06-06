//go:build windows

package main

import "testing"

func TestWindowsTrayIconUsesICOBytes(t *testing.T) {
	if len(trayIconBytes) < 4 {
		t.Fatalf("tray icon is too small: %d bytes", len(trayIconBytes))
	}
	if trayIconBytes[0] != 0x00 || trayIconBytes[1] != 0x00 || trayIconBytes[2] != 0x01 || trayIconBytes[3] != 0x00 {
		t.Fatalf("Windows tray icon must be ICO bytes, got header % x", trayIconBytes[:4])
	}
}
