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
	if settings.SessionExperience != "standard" || settings.ReasoningDisplayMode != "auto" || !settings.ReasoningDisplayModeExplicit {
		t.Fatalf("Settings() legacy mapping = experience:%q reasoning:%q explicit:%t, want standard/auto/true", settings.SessionExperience, settings.ReasoningDisplayMode, settings.ReasoningDisplayModeExplicit)
	}
	if startup.SessionExperience != "standard" || startup.ReasoningDisplayMode != "auto" || !startup.ReasoningDisplayModeExplicit {
		t.Fatalf("DesktopStartupSettings() legacy mapping = experience:%q reasoning:%q explicit:%t, want standard/auto/true", startup.SessionExperience, startup.ReasoningDisplayMode, startup.ReasoningDisplayModeExplicit)
	}
	persisted := config.LoadForEdit(config.UserConfigPath())
	if persisted.DesktopSessionExperience() != "standard" || persisted.DesktopReasoningDisplayMode() != "auto" || !persisted.DesktopReasoningDisplayModeExplicit() {
		t.Fatalf("persisted legacy mapping = %+v, want standard/auto", persisted.Desktop)
	}
}

func TestLegacySessionExperienceSettersMapToCanonicalModes(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()

	if err := app.SetReasoningDisplayMode("expanded"); err != nil {
		t.Fatal(err)
	}
	if got := app.Settings().SessionExperience; got != "deep" {
		t.Fatalf("expanded reasoning = %q, want deep", got)
	}
	for _, apply := range []struct {
		name string
		call func() error
	}{
		{name: "other reasoning", call: func() error { return app.SetReasoningDisplayMode("future-mode") }},
		{name: "display", call: func() error { return app.SetDisplayMode("compact") }},
		{name: "expand thinking", call: func() error { return app.SetExpandThinking(true) }},
	} {
		if err := apply.call(); err != nil {
			t.Fatalf("%s legacy setter: %v", apply.name, err)
		}
		settings := app.Settings()
		if settings.SessionExperience != "standard" || settings.DisplayMode != "standard" || settings.ReasoningDisplayMode != "auto" {
			t.Fatalf("%s mapping = experience:%q display:%q reasoning:%q", apply.name, settings.SessionExperience, settings.DisplayMode, settings.ReasoningDisplayMode)
		}
	}
}
