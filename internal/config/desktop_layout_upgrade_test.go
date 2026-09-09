package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyUserConfigUpgradesOnStartupMigratesClassicDesktopLayout(t *testing.T) {
	path := isolatedDesktopLayoutUserConfigPath(t)
	original := fmt.Sprintf(`config_version = %d

[desktop]
layout_style = "classic"
theme = "dark"
`, Default().ConfigVersion)
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	changed, err := ApplyUserConfigUpgradesOnStartup(path)
	if err != nil {
		t.Fatalf("ApplyUserConfigUpgradesOnStartup: %v", err)
	}
	if !changed {
		t.Fatal("classic desktop layout was not migrated")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `layout_style = "workbench"`) || strings.Contains(text, `layout_style = "classic"`) {
		t.Fatalf("migrated desktop layout not persisted:\n%s", text)
	}
	if !strings.Contains(text, `theme = "dark"`) {
		t.Fatalf("desktop migration dropped an unrelated preference:\n%s", text)
	}
	if got := LoadForEditWithoutCredentials(path).DesktopLayoutStyle(); got != "workbench" {
		t.Fatalf("DesktopLayoutStyle() = %q, want workbench", got)
	}

	again, err := ApplyUserConfigUpgradesOnStartup(path)
	if err != nil || again {
		t.Fatalf("second upgrade changed=%v err=%v", again, err)
	}
}

func TestApplyUserConfigUpgradesOnStartupLeavesCurrentDesktopLayoutsAlone(t *testing.T) {
	for _, layout := range []string{"workbench", "creation"} {
		t.Run(layout, func(t *testing.T) {
			path := isolatedDesktopLayoutUserConfigPath(t)
			original := fmt.Sprintf("config_version = %d\n\n[desktop]\nlayout_style = %q\n", Default().ConfigVersion, layout)
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}

			changed, err := ApplyUserConfigUpgradesOnStartup(path)
			if err != nil || changed {
				t.Fatalf("upgrade changed=%v err=%v", changed, err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != original {
				t.Fatalf("current %s config was rewritten:\n%s", layout, raw)
			}
		})
	}
}

func TestApplyUserConfigUpgradesOnStartupDoesNotRewriteFutureClassicConfig(t *testing.T) {
	path := isolatedDesktopLayoutUserConfigPath(t)
	original := fmt.Sprintf("config_version = %d\n\n[desktop]\nlayout_style = \"classic\"\n", Default().ConfigVersion+1)
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	changed, err := ApplyUserConfigUpgradesOnStartup(path)
	if err != nil || changed {
		t.Fatalf("future config upgrade changed=%v err=%v", changed, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != original {
		t.Fatalf("future config was rewritten:\n%s", raw)
	}
}

func isolatedDesktopLayoutUserConfigPath(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
