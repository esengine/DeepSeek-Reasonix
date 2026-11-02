package serve

import (
	"fmt"
	"testing"

	"reasonix/internal/contract/config"
)

func TestAppearanceCustomWidthSurvivesReloadAndPresetChanges(t *testing.T) {
	zoomHome(t, "")
	initial := zoomRead(t)
	if initial.WidthPercent != 80 || initial.WidthRange != (zoomRangeView{Min: 30, Max: 100, Step: 1}) {
		t.Fatalf("default width: %+v", initial)
	}
	got := zoomSave(t, map[string]any{"width": "custom", "widthPercent": 67})
	if got.Width != "custom" || got.WidthPercent != 67 {
		t.Fatalf("saved width: %+v", got)
	}
	for _, mode := range []string{"standard", "full", "full-all", "custom"} {
		got = zoomSave(t, map[string]any{"width": mode, "readSize": 18, "textSize": 1.2})
		if got.Width != mode || got.WidthPercent != 67 {
			t.Fatalf("preset %s lost width preference: %+v", mode, got)
		}
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Desktop.Appearance.WidthPercent != 67 || cfg.DesktopConversationWidth() != mode {
			t.Fatalf("reload lost preference: %+v", cfg.Desktop)
		}
		if read := zoomRead(t); read.WidthPercent != 67 || read.Width != mode {
			t.Fatalf("GET lost preference: %+v", read)
		}
	}
}

func TestAppearanceWidthPercentClampsOnSaveAndRead(t *testing.T) {
	zoomHome(t, "")
	for _, c := range []struct{ sent, want int }{{-5, 30}, {10, 30}, {110, 100}, {0, 80}, {67, 67}} {
		got := zoomSave(t, map[string]any{"width": "custom", "widthPercent": c.sent})
		if got.WidthPercent != c.want || zoomRead(t).WidthPercent != c.want {
			t.Fatalf("width %d saved as %d, want %d", c.sent, got.WidthPercent, c.want)
		}
	}
	for _, stored := range []int{-1, 0, 120} {
		got := viewOf(config.AppearanceConfig{WidthPercent: stored, Wallpaper: config.WallpaperConfig{File: "test.png"}}, "", "custom")
		if got.WidthPercent != config.EffectiveWidthPercent(stored) || got.WidthRange != (zoomRangeView{Min: 30, Max: 100, Step: 1}) {
			t.Fatalf("wallpaper appearance view: %+v", got)
		}
	}
}

func TestAppearanceWidthPercentUnchangedByUnrelatedSave(t *testing.T) {
	for _, stored := range []int{0, 67} {
		t.Run(fmt.Sprint(stored), func(t *testing.T) {
			zoomHome(t, "")
			if stored != 0 {
				zoomSave(t, map[string]any{"width": "custom", "widthPercent": stored})
			}
			shown := zoomRead(t)
			zoomSave(t, map[string]any{"width": shown.Width, "widthPercent": shown.WidthPercent, "fontUi": "Inter"})
			cfg := config.LoadForEdit(config.UserConfigPath())
			if got := cfg.Desktop.Appearance.WidthPercent; got != stored {
				t.Fatalf("unrelated save changed stored percentage from %d to %d", stored, got)
			}
			zoomSave(t, map[string]any{"width": "custom", "widthPercent": 53})
			if got := config.LoadForEdit(config.UserConfigPath()).Desktop.Appearance.WidthPercent; got != 53 {
				t.Fatalf("deliberate change stored %d, want 53", got)
			}
		})
	}
}
