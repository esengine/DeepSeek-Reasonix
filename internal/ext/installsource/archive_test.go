package installsource

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

// The round trip is the point: what an export produces is what an install
// accepts. If these two ever drift, the archive a user was handed becomes a
// file they cannot do anything with.
func TestInstallAcceptsAnExportedZip(t *testing.T) {
	src := testenv.TempDir(t)
	writeFile(t, filepath.Join(src, "reasonix-plugin.json"), `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "roundtrip",
  "version": "1.0.0",
  "skills": ["skills"],
  "mcpServers": {"docs": {"command": "docs", "env": {"TOKEN": "sk-live-1"}}}
}`)
	writeFile(t, filepath.Join(src, "skills", "greet", "SKILL.md"),
		"---\nname: greet\ndescription: say hello\n---\nHello")

	archive, required, err := pluginpkg.Export("roundtrip", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(required) != 1 || required[0] != "DOCS_TOKEN" {
		t.Fatalf("required = %v", required)
	}
	zipPath := filepath.Join(testenv.TempDir(t), "roundtrip.zip")
	if err := os.WriteFile(zipPath, archive, 0o644); err != nil {
		t.Fatal(err)
	}

	home := testenv.TempDir(t)
	tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: home})
	resp := execInstall(t, tl, map[string]any{"source": zipPath, "kind": "plugin", "apply": true})
	if !resp.OK || resp.Status != "done" {
		t.Fatalf("response = %+v", resp)
	}

	st, err := pluginpkg.LoadState(filepath.Join(home, ".reasonix"))
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Plugins) != 1 || st.Plugins[0].Name != "roundtrip" {
		t.Fatalf("state = %+v", st.Plugins)
	}
	// The credential did not survive the export, so it must not be back after
	// the install either.
	manifest, err := os.ReadFile(filepath.Join(pluginpkg.InstallRoot(filepath.Join(home, ".reasonix"), "roundtrip"), pluginpkg.NativeManifest))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifest), "sk-live-1") {
		t.Fatalf("installed manifest carries the literal credential:\n%s", manifest)
	}
}

func TestZipCannotBeLinked(t *testing.T) {
	zipPath := filepath.Join(testenv.TempDir(t), "x.zip")
	if err := os.WriteFile(zipPath, []byte("PK"), 0o644); err != nil {
		t.Fatal(err)
	}
	tl := &Tool{}
	if _, _, _, err := tl.preparePluginZip(zipPath, "link"); err == nil {
		t.Fatal("link mode accepted an archive, whose unpacked copy is deleted after the call")
	}
}
