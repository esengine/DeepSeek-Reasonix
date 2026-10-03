package serve

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestLinkedPackageAgentInventorySkipsNonresidentSources(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires host privileges")
	}
	for _, tc := range []struct{ label, manifest, body string }{
		{"native", pluginpkg.NativeManifest, `{"apiVersion":"reasonix.io/plugin/v2","name":"resident-agents","contributes":{"agents":["agents"]}}`},
		{"Claude", pluginpkg.ClaudeManifest, `{"name":"resident-agents"}`},
	} {
		t.Run(tc.label, func(t *testing.T) {
			home, _, base := pluginHome(t)
			source, outside := testenv.TempDir(t), testenv.TempDir(t)
			writePluginFile(t, filepath.Join(source, tc.manifest), tc.body)
			writePluginFile(t, filepath.Join(source, "agents", "ordinary.md"), "---\ndescription: Resident fixture\n---\nBODY")
			writePluginFile(t, filepath.Join(outside, "profile.md"), "---\ndescription: External fixture\n---\nEXTERNAL")
			if err := os.Symlink(filepath.Join(outside, "profile.md"), filepath.Join(source, "agents", "outside.md")); err != nil {
				t.Fatal(err)
			}
			pkg, _, err := pluginpkg.ParseDir(source)
			if err != nil {
				t.Fatal(err)
			}
			if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{Name: pkg.Manifest.Name, Root: source, ManifestKind: pkg.ManifestKind, Enabled: true}); err != nil {
				t.Fatal(err)
			}
			resp, err := http.Get(base + "/plugins")
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("installed inventory status=%d", resp.StatusCode)
			}
			plugins := getPlugins(t, base)
			if len(plugins) != 1 || len(plugins[0].Agents) != 1 || plugins[0].Agents[0].Name != "ordinary" {
				t.Fatalf("installed resident-only inventory=%+v", plugins)
			}
		})
	}
}
