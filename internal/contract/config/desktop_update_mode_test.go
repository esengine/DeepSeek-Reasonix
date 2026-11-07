package config

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestUpdateModeDefaultsToNotifyAndOnlyTheThreeNamedModesAreChosen(t *testing.T) {
	for value, want := range map[string]string{"": "notify", "manual": "manual", " manual ": "manual", "notify": "notify", " notify ": "notify", "download": "download", " download ": "download", "Download": "notify", "NOTIFY": "notify", "on": "notify", "auto": "notify", "install": "notify"} {
		cfg := &Config{}
		cfg.Desktop.UpdateMode = value
		if got := cfg.DesktopUpdateMode(); got != want {
			t.Errorf("update_mode %q reads as %q, want %q", value, got, want)
		}
	}
	if (*Config)(nil).DesktopUpdateMode() != "notify" {
		t.Fatal("a missing config must read as the default, notify")
	}
}

func TestSetUpdateModeRefusesWhatItDoesNotOffer(t *testing.T) {
	cfg := &Config{}
	for _, mode := range []string{"auto", "install", "Download", ""} {
		if err := cfg.SetDesktopUpdateMode(mode); !errors.Is(err, ErrUpdateModeInvalid) || cfg.Desktop.UpdateMode != "" {
			t.Fatalf("mode %q: err %v, stored %q; want a refusal that stores nothing", mode, err, cfg.Desktop.UpdateMode)
		}
	}
	if err := cfg.SetDesktopUpdateMode("download"); err != nil || cfg.DesktopUpdateMode() != "download" {
		t.Fatalf("download: %v, reads %q", err, cfg.DesktopUpdateMode())
	}
	if err := cfg.SetDesktopUpdateMode("notify"); err != nil || cfg.DesktopUpdateMode() != "notify" {
		t.Fatalf("notify: %v, reads %q", err, cfg.DesktopUpdateMode())
	}
}

func TestUpdateModeSurvivesARenderAndAnUnchosenModeWritesNothing(t *testing.T) {
	cfg := Default()
	if strings.Contains(RenderTOMLForScope(cfg, RenderScopeUser), "update_mode") {
		t.Fatal("a mode nobody chose is the default and is not written")
	}
	for _, mode := range []string{"manual", "notify", "download"} {
		_ = cfg.SetDesktopUpdateMode(mode)
		if !strings.Contains(RenderTOMLForScope(cfg, RenderScopeUser), `update_mode = "`+mode+`"`) {
			t.Fatalf("%s must be written", mode)
		}
	}
}

func TestAProjectFileCannotChooseTheUpdateMode(t *testing.T) {
	for _, mode := range []string{"manual", "download"} {
		cfg, _ := loadScoped(t, "", "[desktop]\nupdate_mode = \""+mode+"\"\n")
		if cfg.DesktopUpdateMode() != "notify" || !slices.Contains(ignoredKeys(cfg), "desktop.update_mode") {
			t.Fatalf("project %s: mode %q, ignored %v; a project file must not choose it", mode, cfg.DesktopUpdateMode(), ignoredKeys(cfg))
		}
	}
	cfg, _ := loadScoped(t, "[desktop]\nupdate_mode = \"manual\"\n", "[desktop]\nupdate_mode = \"download\"\n")
	if cfg.DesktopUpdateMode() != "manual" {
		t.Fatalf("a project file overrode the user's choice: %q", cfg.DesktopUpdateMode())
	}
}
