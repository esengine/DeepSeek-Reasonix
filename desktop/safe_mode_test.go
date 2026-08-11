package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveTabsWriteNoLongerSkippedBySafeModeEnv(t *testing.T) {
	// v1.20+ ignores REASONIX_SAFE_MODE: tab persistence stays available.
	isolateDesktopUserDirs(t)
	dir := desktopConfigDir()
	entries := []desktopTabEntry{{ID: "t1", Scope: "global"}}

	t.Setenv("REASONIX_SAFE_MODE", "1")
	(&App{}).saveTabsWrite(dir, entries, "t1", 1)
	if _, err := os.Stat(filepath.Join(dir, tabsFileName)); err != nil {
		t.Fatalf("tabs must still write when REASONIX_SAFE_MODE is set: %v", err)
	}
}
