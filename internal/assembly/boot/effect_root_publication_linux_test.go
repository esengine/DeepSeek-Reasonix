//go:build linux

package boot

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/runtime/rootenv"
	"reasonix/internal/session/control"
	"reasonix/internal/state/instruction"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/sessionstore"
)

func TestEffectRootPublicationAutomaticallyIntegratesAndKeepsPersistentScene(t *testing.T) {
	for _, name := range []string{"automatic", "explicit-after-disabled", "recover-interrupted"} {
		t.Run(name, func(t *testing.T) { rootPublicationEffect(t, name) })
	}
}

func rootPublicationEffect(t *testing.T, name string) {
	automatic := name != "explicit-after-disabled"
	home, source, image := statedBootHome(t), testenv.TempDir(t), testenv.VerificationRuntime(t)
	p := &verifiedRootProvider{existing: name == "recover-interrupted"}
	kind := "controlled-root-publication-" + name
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, source, "reasonix.toml", fmt.Sprintf(`default_model="fixture"
[agent]
collaboration="shared"
auto_integrate=%t
verification_runtime=%q
[codegraph]
enabled=false
[[providers]]
name="fixture"
kind=%q
model="fixture"
`, automatic, image, kind))
	writeFile(t, source, "REASONIX.md", "## Reasonix host checks\n\n- verify: [ -f out.md ]\n")
	if p.existing {
		writeFile(t, source, "out.md", "baseline document\n")
	}
	mirrorToUserConfigAt(t, home, source)
	gitInDir(t, source, "init", "-q")
	gitInDir(t, source, "config", "core.autocrlf", "false")
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
	store.WithVerificationChecks([]instruction.VerifyCheck{{Command: "[ -f out.md ]", SourcePath: filepath.Join(source, "REASONIX.md"), Line: 3}})
	path := sessionstore.CanonicalSessionPath(filepath.Join(testenv.TempDir(t), "root.jsonl"))
	lease, err := sessionstore.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	env, err := store.Prepare(t.Context(), path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := lease.IssueWriteAuthority(sessionstore.NextSessionWriteGeneration())
	if err != nil {
		t.Fatal(err)
	}
	if err := env.BindSession(t.Context(), path, authority); err != nil {
		t.Fatal(err)
	}
	live := env.Record().Candidate.WorkspaceRoot
	env.Close()
	var checks atomic.Int32
	controller, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: source, SessionEnvironmentPath: path, SessionDir: filepath.Dir(path),
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ToolResult && e.Tool.Issuer == event.IssuedByHost && e.Tool.Name == "bash" && e.Tool.Err == "" {
				checks.Add(1)
			}
		})})
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	if err := controller.Resume(sessionstore.NewSession("BASE"), path); err != nil {
		t.Fatal(err)
	}
	if err := controller.BindSessionWriteAuthority(lease); err != nil {
		t.Fatal(err)
	}
	controller.SetToolApprovalMode(control.ToolApprovalYolo)
	oldCrash := fileutil.CrashPoint
	defer func() { fileutil.CrashPoint = oldCrash }()
	if name == "recover-interrupted" {
		fileutil.CrashPoint = func(op, path string) {
			if op == "atomic-write" && path == filepath.Join(source, "out.md") {
				panic("source publication interrupted")
			}
		}
	}
	runErr := controller.Run(t.Context(), "write a document")
	fileutil.CrashPoint = oldCrash
	if name == "recover-interrupted" {
		if runErr == nil {
			t.Fatal("publication did not reach source fault boundary")
		}
	} else if runErr != nil {
		t.Fatal(runErr)
	}
	expectedChecks := int32(2)
	if !automatic || name == "recover-interrupted" {
		catalog, err := controller.IsolationResults(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		row := catalog.RootScene
		if row == nil || row.State != "pending" {
			t.Fatalf("manager lost pending export: %+v", row)
		}
		request := isolation.RootSceneRequest{EnvironmentID: row.EnvironmentID, Generation: row.Generation, Runtime: row.Runtime, Owner: row.Owner, PublicationID: row.PublicationID, Action: "recheck"}
		if name == "recover-interrupted" {
			if row.Recovery == nil {
				t.Fatal("missing source recovery preview")
			}
			request.Action, request.Revision = "recover", row.Recovery.Revision
			if err := controller.ManageRootScene(t.Context(), request); err != nil {
				t.Fatal(err)
			}
			request.Action, request.Revision = "recheck", ""
			expectedChecks = 4
		} else {
			if _, err := os.Stat(filepath.Join(source, "out.md")); !os.IsNotExist(err) {
				t.Fatal("disabled automatic integration wrote source", err)
			}
			expectedChecks = 3
		}
		if err := controller.ManageRootScene(t.Context(), request); err != nil {
			t.Fatal(err)
		}
	}
	for _, root := range []string{source, live} {
		if raw, err := os.ReadFile(filepath.Join(root, "out.md")); err != nil || string(raw) != "isolated document\n" {
			t.Fatalf("publication missing at %s: %q %v", root, raw, err)
		}
	}
	r := controller.RootEnvironment().Record()
	if r.Publication == nil || r.Publication.State != "acknowledged" || r.CursorResultID != r.Publication.ResultID || checks.Load() != expectedChecks {
		t.Fatalf("root publication did not close: %+v checks=%d", r.Publication, checks.Load())
	}
	controller.Close()
	if _, err := os.Stat(filepath.Join(live, "out.md")); err != nil {
		t.Fatal("publication deleted the persistent scene", err)
	}
}
