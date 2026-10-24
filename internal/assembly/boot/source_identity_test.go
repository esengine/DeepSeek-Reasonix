package boot

import (
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

func TestBuilderCarriesDiscoveredSourceIdentityIntoCandidateParents(t *testing.T) {
	home, root := statedBootHome(t), testenv.TempDir(t)
	provider.Register("boot-source-parent-identity", func(provider.Config) (provider.Provider, error) { return &persistentRootProvider{}, nil })
	writeFile(t, root, "reasonix.toml", `
default_model = "test-model"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "boot-source-parent-identity"
model = "x"
`)
	writeFile(t, root, "sub/file.txt", "source\n")
	mirrorToUserConfigAt(t, home, root)
	gitInDir(t, root, "init", "-q")
	gitInDir(t, root, "add", "-A")
	gitInDir(t, root, "commit", "-qm", "source")
	for _, directory := range []string{root, filepath.Join(root, "sub")} {
		b := newBuilder()
		built, err := b.build(t.Context(), Options{Home: home, WorkspaceRoot: directory, Sink: event.Discard})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(built.Controller.Close)
		if b.opts.WorkspaceRoot != b.root || b.opts.WorkspaceRepo.WorkTree != b.repo.WorkTree || !b.opts.WorkspaceRepo.Valid() {
			t.Fatalf("candidate parent lost discovered source identity: %+v / %+v", b.opts.WorkspaceRepo, b.repo)
		}
		if pathComparisonKey(b.opts.WorkspaceRepo.WorkTree) != pathComparisonKey(root) {
			t.Fatalf("subdirectory failed to retain the whole source: %s", b.opts.WorkspaceRepo.WorkTree)
		}
	}
}
