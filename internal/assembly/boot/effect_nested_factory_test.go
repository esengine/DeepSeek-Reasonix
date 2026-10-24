package boot

import (
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/session/control"
)

func TestEffectNestedFactoryRegistersAndResumesOnlyItsDirectParent(t *testing.T) {
	home, root := statedBootHome(t), testenv.TempDir(t)
	provider.Register("nested-factory-owner-effect", func(provider.Config) (provider.Provider, error) { return &isolationScriptProvider{}, nil })
	writeFile(t, root, "reasonix.toml", `default_model="fixture"
[agent]
collaboration="shared"
[codegraph]
enabled=false
[[providers]]
name="fixture"
kind="nested-factory-owner-effect"
model="fixture"
`)
	mirrorToUserConfigAt(t, home, root)
	gitInDir(t, root, "init", "-q")
	gitInDir(t, root, "add", "-A")
	gitInDir(t, root, "commit", "-qm", "fixture")
	b := newBuilder()
	built, err := b.build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer built.Controller.Close()
	built.Controller.SetToolApprovalMode(control.ToolApprovalYolo)
	store := b.tools.isolation.store
	parentEntry, err := store.BeginOwned(t.Context(), root, "source-session")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Abort(t.Context(), parentEntry) })
	parent := b.opts
	parent.frozenConfig, err = b.cfg.CloneResolved()
	if err != nil {
		t.Fatal(err)
	}
	parent.DelegationScheduler, parent.SessionDir = b.tools.taskTool.Scheduler(), b.session.dir
	parent.isolatedParent = &isolatedParent{store: store, entry: parentEntry, source: root}
	children := isolation.NewStore(filepath.Join(b.roots.DeliveryWorktreeDir(), "isolated"), parentEntry.Cand.WorkspaceRoot, parentEntry.Cand.Repo).
		WithMutationCoordinator(b.session.mutations)
	grant, err := writeclaim.NormalizeWritePaths(parentEntry.Cand.WorkspaceRoot, []string{"child.txt"})
	if err != nil {
		t.Fatal(err)
	}
	spec := delegation.ProfileExecSpec{Grant: delegation.CapabilityGrant{WritePaths: grant}}
	factory := newIsolatedEnvironmentFactory(parent, children, b.tools.isolation.posture, parentEntry.Cand.WorkspaceRoot, event.Discard, nil)
	ctx := agent.WithParentSession(t.Context(), "source-session")
	first, err := factory(ctx, spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Finish(ctx, "retained", nil); err != nil {
		t.Fatal(err)
	}
	catalog, err := store.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range catalog.Results {
		if row.ID == first.ID {
			found = row.ParentID == parentEntry.ID && row.TargetWorkspace == parentEntry.Cand.WorkspaceRoot
		}
	}
	if !found {
		t.Fatal("real candidate Build did not register its direct parent")
	}
	second, err := factory(ctx, spec, &delegation.EnvironmentSource{ID: first.ID})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.Generation == first.Generation {
		t.Fatal("nested continuation lost its environment identity or run generation")
	}
	if _, err := second.Finish(ctx, "retained", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Finish(ctx, parentEntry); err == nil {
		t.Fatal("parent sealed while its registered child result remains pending")
	}
	if resumed, err := factory(ctx, spec, &delegation.EnvironmentSource{ID: first.ID}); err == nil {
		_, _ = resumed.Finish(ctx, "", nil)
		t.Fatal("nested factory resumed after direct parent execution stopped")
	}
}
