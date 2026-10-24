package boot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/runtime/rootenv"
	"reasonix/internal/state/mutation"
)

// A host preparing a new root environment first reclaims the checkouts earlier
// preparations abandoned, and reports the one that still holds unintegrated work.
func TestEffectRootPreparationReclaimsAbandonedEnvironments(t *testing.T) {
	home, root := statedBootHome(t), testenv.TempDir(t)
	provider.Register("boot-root-reclaim", func(provider.Config) (provider.Provider, error) { return &persistentRootProvider{}, nil })
	writeFile(t, root, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "BASE"
collaboration = "isolated"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "boot-root-reclaim"
model = "x"
`)
	mirrorToUserConfigAt(t, home, root)
	gitInDir(t, root, "init", "-q")
	gitInDir(t, root, "config", "core.autocrlf", "false")
	gitInDir(t, root, "add", "-A")
	gitInDir(t, root, "commit", "-qm", "base")
	repo, err := gitcmd.Open(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	coord, err := mutation.New(root, config.WorkspaceLeaseDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	store, err := rootenv.New(config.RootsForHome(home).DeliveryWorktreeDir(), root, repo, coord)
	if err != nil {
		t.Fatal(err)
	}
	late, err := store.Prepare(t.Context(), "abandoned-late", []string{"../outside"}, nil)
	if err == nil {
		t.Fatal("unsafe environment copy was prepared")
	}
	dirty, err := store.Prepare(t.Context(), "abandoned-dirty", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dirty.Record().Candidate.WorkspaceRoot, "work.txt", "not integrated\n")
	dirty.Close()
	lateRoot, dirtyRoot := late.Record().Candidate.WorktreeRoot, dirty.Record().Candidate.WorktreeRoot
	if _, err := os.Stat(lateRoot); err != nil {
		t.Fatal("late failure left no checkout:", err)
	}
	sink := &noticeSink{}
	opts := Options{Home: home, WorkspaceRoot: root, Sink: sink}
	ctrl, err := Build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	bindPresenceSession(t, ctrl)
	release, err := ctrl.BeginRuntimeReplacement()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	prepared, err := PrepareRootExecution(t.Context(), ctrl, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if _, err := os.Stat(filepath.Dir(filepath.Dir(lateRoot))); !os.IsNotExist(err) {
		t.Fatal("abandoned checkout survived the next preparation:", err)
	}
	if refs := gitInDir(t, root, "for-each-ref", "refs/reasonix/root-environments/"+late.Record().ID); refs != "" {
		t.Fatalf("abandoned anchors survived: %s", refs)
	}
	if raw, err := os.ReadFile(filepath.Join(dirty.Record().Candidate.WorkspaceRoot, "work.txt")); err != nil || string(raw) != "not integrated\n" {
		t.Fatalf("unintegrated work = %q %v", raw, err)
	}
	if _, err := os.Stat(dirtyRoot); err != nil {
		t.Fatal(err)
	}
	reported := false
	sink.mu.Lock()
	for _, e := range sink.ev {
		reported = reported || (e.Kind == event.Notice && strings.Contains(e.Detail, dirty.Record().ID))
	}
	sink.mu.Unlock()
	if !reported {
		t.Fatal("retained environment was not reported")
	}
	if prepared.Result == nil || prepared.Controller.WorkspaceRepo().WorkTree == ctrl.WorkspaceRepo().WorkTree {
		t.Fatal("reclaim kept the new preparation from running")
	}
}
