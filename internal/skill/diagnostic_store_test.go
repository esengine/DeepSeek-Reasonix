package skill

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/pluginpkg"
)

func TestDiagnosticStorePreservesPluginOwnership(t *testing.T) {
	rh, root := t.TempDir(), t.TempDir()
	t.Setenv("REASONIX_HOME", rh)
	for _, owner := range []string{"owner-a", "owner-b"} {
		base := filepath.Join(rh, "plugins", owner)
		name := owner + "-probe"
		files := map[string]string{
			pluginpkg.CodexManifest:        `{"name":"` + owner + `","skills":"skills"}`,
			"skills/" + name + "/SKILL.md": "---\nname: " + name + "\ndescription: Probe\nallowed-tools: [search]\n---\nTest\n",
		}
		for relative, body := range files {
			file := filepath.Join(base, relative)
			if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err := pluginpkg.Upsert(rh, pluginpkg.InstalledPlugin{Name: owner, Root: filepath.Join("plugins", owner), ManifestKind: "codex", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string]bool{}
	for _, sk := range DiagnosticStore(root, t.TempDir(), rh, cfg).List() {
		if sk.Name == "owner-a-probe" || sk.Name == "owner-b-probe" {
			owners[sk.Plugin] = true
		}
	}
	if len(owners) != 2 || !owners["owner-a"] || !owners["owner-b"] {
		t.Fatalf("plugin skills lost ownership: %v", owners)
	}
}
