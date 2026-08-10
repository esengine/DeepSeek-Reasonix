package main

import (
	"testing"

	"reasonix/internal/config"
)

func TestReasoningDisplayDefaultsAndPersistsUserSelection(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	settings := app.Settings()
	startup := app.DesktopStartupSettings()
	if settings.DisplayMode != "standard" || settings.ReasoningDisplayMode != "auto" || settings.ReasoningDisplayModeExplicit {
		t.Fatalf("Settings() defaults = display:%q reasoning:%q explicit:%t, want standard/auto/false", settings.DisplayMode, settings.ReasoningDisplayMode, settings.ReasoningDisplayModeExplicit)
	}
	if startup.DisplayMode != "standard" || startup.ReasoningDisplayMode != "auto" || startup.ReasoningDisplayModeExplicit {
		t.Fatalf("DesktopStartupSettings() defaults = display:%q reasoning:%q explicit:%t, want standard/auto/false", startup.DisplayMode, startup.ReasoningDisplayMode, startup.ReasoningDisplayModeExplicit)
	}

	if err := app.SetReasoningDisplayMode("summary"); err != nil {
		t.Fatalf("SetReasoningDisplayMode(summary): %v", err)
	}
	settings = app.Settings()
	startup = app.DesktopStartupSettings()
	if settings.ReasoningDisplayMode != "summary" || !settings.ReasoningDisplayModeExplicit {
		t.Fatalf("Settings() user selection = reasoning:%q explicit:%t, want summary/true", settings.ReasoningDisplayMode, settings.ReasoningDisplayModeExplicit)
	}
	if startup.ReasoningDisplayMode != "summary" || !startup.ReasoningDisplayModeExplicit {
		t.Fatalf("DesktopStartupSettings() user selection = reasoning:%q explicit:%t, want summary/true", startup.ReasoningDisplayMode, startup.ReasoningDisplayModeExplicit)
	}
	persisted := config.LoadForEdit(config.UserConfigPath())
	if persisted.DesktopReasoningDisplayMode() != "summary" || !persisted.DesktopReasoningDisplayModeExplicit() {
		t.Fatalf("persisted user selection = %+v, want explicit summary", persisted.Desktop)
	}
}
