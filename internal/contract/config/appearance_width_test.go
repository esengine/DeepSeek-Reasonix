package config

import (
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestAppearanceWidthPercentRoundTrip(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "config.toml")
	cfg := LoadForEdit(path)
	for _, mode := range []string{"custom", "standard", "full", "full-all"} {
		if err := cfg.SetDesktopConversationWidth(mode); err != nil {
			t.Fatal(err)
		}
		cfg.Desktop.Appearance.WidthPercent = 67
		if err := cfg.SaveToScope(path, RenderScopeUser); err != nil {
			t.Fatal(err)
		}
		cfg = LoadForEdit(path)
		if cfg.DesktopConversationWidth() != mode || cfg.Desktop.Appearance.WidthPercent != 67 {
			t.Fatalf("round trip %s: width=%s appearance=%+v", mode, cfg.DesktopConversationWidth(), cfg.Desktop.Appearance)
		}
	}
}

func TestEffectiveWidthPercentBounds(t *testing.T) {
	for _, c := range []struct{ in, want int }{{0, 80}, {-1, 30}, {1, 30}, {30, 30}, {67, 67}, {100, 100}, {101, 100}} {
		if got := EffectiveWidthPercent(c.in); got != c.want {
			t.Errorf("EffectiveWidthPercent(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestWidthPercentSaveClampsManualValues(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "config.toml")
	for _, c := range []struct{ in, want int }{{-1, 30}, {120, 100}, {0, 0}} {
		cfg := LoadForEdit(path)
		cfg.Desktop.Appearance.WidthPercent = c.in
		if err := cfg.SaveToScope(path, RenderScopeUser); err != nil {
			t.Fatal(err)
		}
		if got := LoadForEdit(path).Desktop.Appearance.WidthPercent; got != c.want {
			t.Errorf("saved percentage %d = %d, want %d", c.in, got, c.want)
		}
	}
}
