package installsource

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestRemovingAPluginReclaimsItsSupersededCopies(t *testing.T) {
	home := testenv.TempDir(t)
	dir := pluginpkg.PluginsDir(home)
	mk := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	canonical := mk("kit")
	older := mk(".kit.staging-111")
	active := mk(".kit.staging-222")
	other := mk(".kit.staging-x.staging-333")
	if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{Name: "kit.staging-x", Root: pluginpkg.RelativeRoot(home, other)}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(active); err != nil {
		t.Fatal(err)
	}
	removeSupersededPluginCopies(home, dir, "kit")
	for _, gone := range []string{canonical, older} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Fatalf("%s survived the removal: %v", filepath.Base(gone), err)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("another plugin's directory was removed: %v", err)
	}
}
