package pluginspec

import (
	"path/filepath"
	"reasonix/internal/contract/config"
	"testing"
)

func TestCompositionPackagePathsRebaseWithoutMutatingSiblingSpecs(t *testing.T) {
	root, private := t.TempDir(), t.TempDir()
	spec := FromEntry(config.PluginEntry{Name: "docs", Command: filepath.Join(root, "server"), Args: []string{"--config=" + filepath.Join(root, "config.json")}, Env: map[string]string{"ASSET": filepath.Join(root, "asset")}}, root, Options{})
	before := spec
	ApplySessionPackageRoot(&spec, map[string]SessionPackageRoot{"docs": {Retained: root, Private: private}})
	if spec.Command != filepath.Join(private, "server") || spec.Args[0] != "--config="+filepath.Join(private, "config.json") || spec.Env["ASSET"] != filepath.Join(private, "asset") {
		t.Fatalf("private paths not applied: %+v", spec)
	}
	if before.Args[0] != "--config="+filepath.Join(root, "config.json") || before.Env["ASSET"] != filepath.Join(root, "asset") {
		t.Fatal("sibling spec changed")
	}
}

func TestCompositionSeparatesProcessStateAndSharesCredentialReference(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	entry := config.PluginEntry{Name: "docs", Command: "docs-server"}
	a := FromEntry(entry, root, Options{StateHome: home, ProcessScope: "session-a"})
	b := FromEntry(entry, root, Options{StateHome: home, ProcessScope: "session-b"})
	if a.StateDir != b.StateDir {
		t.Fatal("credential reference stopped sharing its existing owner")
	}
	if a.ProcessStateDir == "" || a.ProcessStateDir == b.ProcessStateDir {
		t.Fatal("session process state was shared")
	}
}
