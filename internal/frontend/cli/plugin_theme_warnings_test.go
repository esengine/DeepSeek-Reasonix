package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestPluginDoctorReportsThemeWarningsWhenDisabled(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	root := filepath.Join(home, "plugins", "theme-warning-kit")
	writePluginTestFile(t, filepath.Join(root, pluginpkg.NativeManifest), `{"apiVersion":"reasonix.io/plugin/v2","name":"theme-warning-kit","contributes":{"themes":["themes/*/theme.json"]}}`)
	writePluginTestFile(t, filepath.Join(root, "themes", "broken", "theme.json"), `{"schemaVersion":99,"tokens":{"light":{"bg":"#FFFFFF"},"dark":{"bg":"#111111"}}}`)
	writePluginTestFile(t, filepath.Join(root, "themes", "partial", "theme.json"), `{"schemaVersion":1,"tokens":{"light":{"bg":"#FFFFFF","sidebar":"#111111"},"dark":{"bg":"#111111"}}}`)
	for _, enabled := range []bool{true, false} {
		if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{Name: "theme-warning-kit", Root: "plugins/theme-warning-kit", ManifestKind: "reasonix", Enabled: enabled}); err != nil {
			t.Fatal(err)
		}
		out := captureStdout(t, func() {
			if rc := pluginCommand([]string{"doctor", "theme-warning-kit"}); rc != 0 {
				t.Fatalf("doctor rc=%d", rc)
			}
		})
		for _, want := range []string{"warning:", "themes/broken/theme.json", "unsupported schemaVersion 99", "themes/partial/theme.json", `"sidebar" is not a theme token`} {
			if !strings.Contains(out, want) {
				t.Errorf("enabled=%v doctor missing %q:\n%s", enabled, want, out)
			}
		}
		if strings.Contains(out, "no enabled plugin") {
			t.Errorf("disabled package was diagnosed through the enabled registry: %s", out)
		}
	}
}
