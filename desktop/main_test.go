package main

import (
	"os"
	"testing"
)

// TestMain isolates os.UserConfigDir() for the whole package. On Windows it
// reads %AppData%, which the per-test HOME / XDG_CONFIG_HOME overrides do not
// cover — without this, tests that persist desktop state (saveWorkspace,
// session/cache writes) leak into the developer's real Reasonix config dir.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "reasonix-desktop-test")
	if err != nil {
		os.Exit(1)
	}
	os.Setenv("AppData", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
