package main

import (
	"strings"
	"testing"

	"reasonix/internal/config"
)

func TestHideAmountsPersistsAcrossRestart(t *testing.T) {
	t.Setenv("REASONIX_HOME", "")
	isolateDesktopUserDirs(t)
	app := NewApp()
	if app.Settings().HideAmounts || app.DesktopStartupSettings().HideAmounts {
		t.Fatal("amounts should remain visible by default")
	}
	for _, hidden := range []bool{true, false} {
		if err := app.SetHideAmounts(hidden); err != nil {
			t.Fatalf("SetHideAmounts(%t): %v", hidden, err)
		}
		restarted := NewApp()
		if restarted.Settings().HideAmounts != hidden || restarted.DesktopStartupSettings().HideAmounts != hidden {
			t.Fatalf("restarted settings did not preserve hideAmounts=%t", hidden)
		}
		cfg := config.LoadForEdit(config.UserConfigPath())
		if cfg.Desktop.HideAmounts != hidden {
			t.Fatalf("saved desktop.hide_amounts=%t, want %t", cfg.Desktop.HideAmounts, hidden)
		}
		if strings.Contains(config.RenderTOMLForScope(cfg, config.RenderScopeProject), "hide_amounts") {
			t.Fatal("desktop amount privacy must not be written to project config")
		}
	}
}
