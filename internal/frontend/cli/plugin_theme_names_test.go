package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestPluginShowNamesDirectoryThemePacksWhenDisabled(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	root := filepath.Join(home, "plugins", "palette-duo")
	writePluginTestFile(t, filepath.Join(root, pluginpkg.NativeManifest), `{"apiVersion":"reasonix.io/plugin/v2","name":"palette-duo","contributes":{"themes":["themes/*/theme.json"]}}`)
	for _, name := range []string{"dawn", "midnight"} {
		writePluginTestFile(t, filepath.Join(root, "themes", name, "theme.json"), "{}")
	}
	for _, enabled := range []bool{true, false} {
		if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{Name: "palette-duo", Root: "plugins/palette-duo", ManifestKind: "reasonix", Enabled: enabled}); err != nil {
			t.Fatal(err)
		}
		out := captureStdout(t, func() {
			if rc := pluginCommand([]string{"show", "palette-duo"}); rc != 0 {
				t.Fatalf("show rc=%d", rc)
			}
		})
		for _, name := range []string{"dawn", "midnight"} {
			want := "  " + name + "\t" + filepath.Join(root, "themes", name, "theme.json")
			if !strings.Contains(out, want) {
				t.Errorf("enabled=%v show missing %q:\n%s", enabled, want, out)
			}
		}
		if !strings.Contains(out, "themes: 2") {
			t.Errorf("show changed the theme count: %s", out)
		}
	}
}
