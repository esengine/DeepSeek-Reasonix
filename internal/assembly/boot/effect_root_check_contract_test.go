package boot

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/runtime/rootenv"
	"reasonix/internal/state/instruction"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/sessionstore"
)

func TestEffectRootRestoreCannotReplaceCapturedChecksFromWritableScene(t *testing.T) {
	home, source := statedBootHome(t), testenv.TempDir(t)
	provider.Register("restored-root-check-contract", func(provider.Config) (provider.Provider, error) { return &persistentRootProvider{}, nil })
	writeFile(t, source, "reasonix.toml", `default_model="fixture"
[agent]
collaboration="shared"
[codegraph]
enabled=false
[[providers]]
name="fixture"
kind="restored-root-check-contract"
model="fixture"
`)
	writeFile(t, source, "REASONIX.md", "## Reasonix host checks\n\n- verify: false\n")
	mirrorToUserConfigAt(t, home, source)
	gitInDir(t, source, "init", "-q")
	gitInDir(t, source, "add", "-A")
	gitInDir(t, source, "commit", "-qm", "fixture")
	repo, err := gitcmd.Open(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := mutation.New(source, config.WorkspaceLeaseDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	store, err := rootenv.New(config.RootsForHome(home).DeliveryWorktreeDir(), source, repo, coordinator)
	if err != nil {
		t.Fatal(err)
	}
	store.WithVerificationChecks([]instruction.VerifyCheck{{Command: "false", SourcePath: filepath.Join(source, "REASONIX.md"), Line: 3}})
	path := sessionstore.CanonicalSessionPath(filepath.Join(testenv.TempDir(t), "root.jsonl"))
	lease, err := sessionstore.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	authority, err := lease.IssueWriteAuthority(sessionstore.NextSessionWriteGeneration())
	if err != nil {
		t.Fatal(err)
	}
	env, err := store.Prepare(t.Context(), path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.BindSession(t.Context(), path, authority); err != nil {
		t.Fatal(err)
	}
	live := env.Record().Candidate.WorkspaceRoot
	writeFile(t, live, "out.md", "candidate\n")
	p, err := env.BeginPublication(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, live, "REASONIX.md", "## Reasonix host checks\n\n- verify: true\n")
	env.Close()
	for index := range 2 {
		b := newBuilder()
		built, err := b.build(t.Context(), Options{Home: home, WorkspaceRoot: source, SessionEnvironmentPath: path, SessionDir: filepath.Dir(path), Sink: event.Discard})
		if err != nil {
			t.Fatal(err)
		}
		controller := built.Controller
		if len(b.prompt.projectChecks) != 1 || b.prompt.projectChecks[0].Command != "false" {
			t.Fatalf("restored runtime selected scene checks: %+v", b.prompt.projectChecks)
		}
		r := controller.RootEnvironment().Record()
		if r.Publication == nil || r.Publication.ID != p.ID || r.Publication.State != "pending" {
			t.Fatalf("restart replaced export: %+v", r.Publication)
		}
		if index == 1 {
			if err := controller.Resume(sessionstore.NewSession("BASE"), path); err != nil {
				t.Fatal(err)
			}
			if err := controller.BindSessionWriteAuthority(lease); err != nil {
				t.Fatal(err)
			}
			catalog, err := controller.IsolationResults(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			row := catalog.RootScene
			canonicalSource, err := filepath.EvalSymlinks(source)
			if err != nil {
				t.Fatal(err)
			}
			if row == nil || row.SourceWorkspace != canonicalSource || row.LiveWorkspace != live || row.PublicationID != p.ID {
				t.Fatalf("manager lost source/live identity: %+v", row)
			}
			request := isolation.RootSceneRequest{EnvironmentID: row.EnvironmentID, Generation: row.Generation, Runtime: "stale", Owner: row.Owner, PublicationID: row.PublicationID, Action: "retry"}
			if err := controller.ManageRootScene(t.Context(), request); err == nil {
				t.Fatal("stale manager request accepted")
			}
			request.Runtime = row.Runtime
			if err := controller.ManageRootScene(t.Context(), request); err != nil {
				t.Fatal(err)
			}
			if controller.RootEnvironment().Record().Publication != nil {
				t.Fatal("retry retained active publication")
			}
			if err := controller.ManageRootScene(t.Context(), request); err == nil {
				t.Fatal("replayed retry accepted against successor")
			}
			if _, err := os.Stat(filepath.Join(live, "out.md")); err != nil {
				t.Fatal("retry deleted live scene", err)
			}
		}
		controller.Close()
	}
}
