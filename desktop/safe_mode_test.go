package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopStartupSettingsExposeSafeMode(t *testing.T) {
	t.Setenv("REASONIX_SAFE_MODE", "1")
	view := NewApp().DesktopStartupSettings()
	if !view.SafeMode {
		t.Fatalf("startup settings = %+v", view)
	}
}

func TestSaveTabsWriteSkipsInSafeMode(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := desktopConfigDir()
	entries := []desktopTabEntry{{ID: "t1", Scope: "global"}}

	t.Setenv("REASONIX_SAFE_MODE", "1")
	(&App{}).saveTabsWrite(dir, entries, "t1", 1)
	if _, err := os.Stat(filepath.Join(dir, tabsFileName)); !os.IsNotExist(err) {
		t.Fatalf("safe mode wrote desktop-tabs.json (stat err=%v)", err)
	}

	t.Setenv("REASONIX_SAFE_MODE", "")
	(&App{}).saveTabsWrite(dir, entries, "t1", 1)
	if _, err := os.Stat(filepath.Join(dir, tabsFileName)); err != nil {
		t.Fatalf("normal mode must write desktop-tabs.json: %v", err)
	}
}
