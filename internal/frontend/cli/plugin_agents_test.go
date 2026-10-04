package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestPluginShowAgentOnlyPackage(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	root := filepath.Join(home, "plugins", "orientation")
	writePluginTestFile(t, filepath.Join(root, pluginpkg.NativeManifest), `{"apiVersion":"reasonix.io/plugin/v2","name":"orientation","contributes":{"agents":["agents"]}}`)
	writePluginTestFile(t, filepath.Join(root, "agents", "map.md"), "---\nname: map\ndescription: Map the\n  selected files\n---\nRead the files.")
	writePluginTestFile(t, filepath.Join(root, "agents", "check.md"), "---\nname: check\n---\nCheck the files.")
	for _, enabled := range []bool{true, false} {
		if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{Name: "orientation", Root: "plugins/orientation", ManifestKind: "reasonix", Enabled: enabled}); err != nil {
			t.Fatal(err)
		}
		out := captureStdout(t, func() {
			if rc := pluginCommand([]string{"show", "orientation"}); rc != 0 {
				t.Fatalf("plugin show rc = %d", rc)
			}
		})
		for _, want := range []string{"agents: 2", "agents:\n", "/orientation:agent:map\tMap the selected files", "/orientation:agent:check\t(no description)"} {
			if !strings.Contains(out, want) {
				t.Errorf("enabled=%t: show missing %q:\n%s", enabled, want, out)
			}
		}
	}
}
