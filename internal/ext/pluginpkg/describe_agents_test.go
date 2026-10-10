package pluginpkg

import (
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestInstalledAgentOnlyText(t *testing.T) {
	for _, tc := range []struct {
		kind, manifest, body string
	}{
		{"reasonix", NativeManifest, `{"apiVersion":"reasonix.io/plugin/v2","name":"orientation","contributes":{"agents":["agents"]}}`},
		{"claude", ClaudeManifest, `{"name":"orientation"}`},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			home := testenv.TempDir(t)
			root := filepath.Join(home, "plugins", "orientation")
			writeTestFile(t, filepath.Join(root, tc.manifest), tc.body)
			writeTestFile(t, filepath.Join(root, "agents", "map.md"), "---\nname: map\ndescription: Map the\n  selected files\n---\nRead the files.")
			writeTestFile(t, filepath.Join(root, "agents", "check.md"), "---\nname: check\n---\nCheck the selected files.")
			for _, enabled := range []bool{true, false} {
				if err := Upsert(home, InstalledPlugin{Name: "orientation", Root: "plugins/orientation", ManifestKind: tc.kind, Enabled: enabled}); err != nil {
					t.Fatal(err)
				}
				list, err := InstalledListText(home)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(list, "2 agents") || strings.Contains(list, "no exported capabilities") {
					t.Errorf("enabled=%t: list omits agent capabilities:\n%s", enabled, list)
				}
				show, err := InstalledShowText(home, "orientation")
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range []string{"2 agents", "agents:\n", "/orientation:agent:map - Map the selected files", "/orientation:agent:check - (no description)"} {
					if !strings.Contains(show, want) {
						t.Errorf("enabled=%t: show missing %q:\n%s", enabled, want, show)
					}
				}
				if strings.Contains(show, "no detailed inventory available") {
					t.Errorf("enabled=%t: agent inventory reported empty:\n%s", enabled, show)
				}
				if !enabled && !strings.Contains(show, "enable this plugin before") {
					t.Errorf("disabled agents are presented as active:\n%s", show)
				}
			}
		})
	}
}
