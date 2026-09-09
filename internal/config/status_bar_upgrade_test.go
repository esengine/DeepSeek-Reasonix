package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusBarIconDefaultUpgradePreservesLaterChoice(t *testing.T) {
	home := isolateUserConfigHome(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	path := UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[desktop]\nstatus_bar_style = \"text\"\ntheme = \"dark\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := LoadForEdit(path)
	if cfg.DesktopStatusBarStyle() != "icon" {
		t.Fatal("legacy text setting must adopt the new icon default")
	}
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	cfg = LoadForEdit(path)
	if !cfg.Desktop.StatusBarStyleInitialized || cfg.DesktopStatusBarStyle() != "icon" || cfg.DesktopTheme() != "dark" {
		t.Fatal("save must persist the migration and preserve unrelated preferences")
	}
	if err := cfg.SetDesktopStatusBarStyle("text"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := cfg.SaveTo(path); err != nil {
			t.Fatal(err)
		}
		cfg = LoadForEdit(path)
		if cfg.DesktopStatusBarStyle() != "text" {
			t.Fatal("later manual text choice must survive repeated reloads")
		}
	}
	if strings.Contains(RenderTOMLForScope(cfg, RenderScopeProject), "status_bar_style_initialized") {
		t.Fatal("desktop migration marker must not enter project configuration")
	}
}
