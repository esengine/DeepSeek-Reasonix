package config

import (
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

// render.go is hand-written, so a field it does not know about is dropped on
// the next save. That is how an uploaded wallpaper landed on disk and then
// disappeared from the config: the bytes were there, the reference was not.
func TestAppearanceSurvivesSaveAndLoad(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "config.toml")
	cfg := LoadForEdit(path)
	cfg.Desktop.Appearance.Zoom = 1.2
	cfg.Desktop.Appearance.ReadSize = 15
	cfg.Desktop.Appearance.FontUI = "Inter"
	cfg.Desktop.Appearance.Wallpaper = WallpaperConfig{File: "a1b2.png", Opacity: 0.5, Dim: 0.55, FocusX: 0.4, FocusY: 0.6}
	// The [desktop] table only belongs to a user config, so ask for that scope
	// rather than letting the temp path be read as a project's.
	if err := cfg.SaveToScope(path, RenderScopeUser); err != nil {
		t.Fatal(err)
	}

	back := LoadForEdit(path)
	got := back.Desktop.Appearance
	if got.Wallpaper.File != "a1b2.png" {
		t.Fatalf("wallpaper file = %q, want it to survive the round trip", got.Wallpaper.File)
	}
	if got.Wallpaper.Opacity != 0.5 || got.Wallpaper.Dim != 0.55 || got.Wallpaper.FocusX != 0.4 || got.Wallpaper.FocusY != 0.6 {
		t.Fatalf("wallpaper settings = %+v, want the values saved", got.Wallpaper)
	}
	if got.Zoom != 1.2 || got.ReadSize != 15 || got.FontUI != "Inter" {
		t.Fatalf("appearance = %+v, want zoom/read size/font kept", got)
	}

	// Saving again must not lose what the first save wrote.
	if err := back.SaveToScope(path, RenderScopeUser); err != nil {
		t.Fatal(err)
	}
	if again := LoadForEdit(path).Desktop.Appearance; again.Wallpaper.File != "a1b2.png" || again.Zoom != 1.2 {
		t.Fatalf("second save lost fields: %+v", again)
	}
}
