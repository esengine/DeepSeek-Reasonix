package serve

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
	"reasonix/internal/state/agentpreset"
	"reasonix/internal/state/sessionstore"
)

func compositionSwitchFixture(t *testing.T) (*Server, *control.Controller) {
	t.Helper()
	home, root, dir := testenv.TempDir(t), testenv.TempDir(t), testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	d := agentcomposition.Builtins()[0]
	d.ID = "user:alternate"
	if _, err := agentpreset.New(home, root).Save(d, ""); err != nil {
		t.Fatal(err)
	}
	build := func(_ context.Context, opts boot.Options) (*boot.BuildResult, error) {
		store := agentpreset.New(home, root)
		id := opts.AgentComposition
		if id == "default" {
			id, _ = store.Default()
		}
		session := sessionstore.NewSession("standard prompt")
		if id != "" && id != "builtin:general" {
			v, err := store.Get(id)
			if err != nil {
				return nil, err
			}
			binding := &agentcomposition.Binding{Version: v, Resolved: v.Definition, Workspace: root}
			binding.Seal()
			session.BindComposition(binding)
		}
		exec := agent.New(nil, tool.NewRegistry(), session, agent.Options{}, event.Discard)
		ctrl := control.New(control.Options{Executor: exec, Runner: exec, Sink: event.Discard, WorkspaceRoot: root, SessionDir: dir})
		ctrl.ConfigureCompositions(home, nil)
		return &boot.BuildResult{Controller: ctrl}, nil
	}
	first, err := build(t.Context(), boot.Options{AgentComposition: "builtin:minimal"})
	if err != nil {
		t.Fatal(err)
	}
	old := first.Controller
	old.SetSessionPath(filepath.Join(dir, "blank.jsonl"))
	if err := old.SnapshotRewrite(); err != nil {
		t.Fatal(err)
	}
	s := New(old, NewBroadcaster(), config.ServeConfig{})
	s.compositionAssembly = &compositionAssembly{build: build}
	t.Cleanup(func() { s.ctl().Close(); old.Close() })
	closeSharedCatalogsOnCleanup(t)
	return s, old
}

func TestCompositionSwitchPersistsBeforePublicationAndUsesNewDefault(t *testing.T) {
	s, old := compositionSwitchFixture(t)
	if err := old.SelectAgentComposition(t.Context(), "user:alternate"); err != nil {
		t.Fatal(err)
	}
	next := s.ctl().(*control.Controller)
	if next == old || next.CompositionBinding().Version.Definition.ID != "user:alternate" {
		t.Fatal("replacement not published")
	}
	loaded, err := sessionstore.LoadSession(next.SessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Composition().Digest != next.CompositionBinding().Digest {
		t.Fatal("publication preceded binding persistence")
	}
	s.compositionAssembly.build = func(context.Context, boot.Options) (*boot.BuildResult, error) {
		t.Fatal("same version rebuilt")
		return nil, nil
	}
	if err := next.SelectAgentComposition(t.Context(), "user:alternate"); err != nil {
		t.Fatal(err)
	}
}

func TestCompositionSwitchToStandardRestoresLegacyBlankSession(t *testing.T) {
	s, old := compositionSwitchFixture(t)
	if err := old.SelectAgentComposition(t.Context(), "builtin:general"); err != nil {
		t.Fatal(err)
	}
	next := s.ctl().(*control.Controller)
	if next == old || next.CompositionBinding() != nil {
		t.Fatal("Standard retained an implicit binding")
	}
	data, err := os.ReadFile(sessionstore.SessionEventLogPath(next.SessionPath()))
	if err != nil || bytes.Contains(data, []byte(`"schema_version":4`)) {
		t.Fatalf("blank Standard session retained the composition format: %v", err)
	}
	s.compositionAssembly.build = func(context.Context, boot.Options) (*boot.BuildResult, error) {
		t.Fatal("unbound Standard selection rebuilt")
		return nil, nil
	}
	if err := next.SelectAgentComposition(t.Context(), "builtin:general"); err != nil {
		t.Fatal(err)
	}
}

func TestUnboundCompositionSwitchRollbackRemovesUnpublishedBinding(t *testing.T) {
	s, old := compositionSwitchFixture(t)
	if err := old.SelectAgentComposition(t.Context(), "builtin:general"); err != nil {
		t.Fatal(err)
	}
	standard := s.ctl().(*control.Controller)
	s.compositionAssembly.persist = func(next *control.Controller) error {
		if err := next.SnapshotRewrite(); err != nil {
			return err
		}
		return errors.New("publish persistence failed")
	}
	if err := standard.SelectAgentComposition(t.Context(), "user:alternate"); err == nil {
		t.Fatal("candidate failure hidden")
	}
	loaded, err := sessionstore.LoadSession(standard.SessionPath())
	if err != nil || loaded.Composition() != nil || s.ctl() != standard {
		t.Fatalf("failed switch changed unbound session: %v", err)
	}
}

func TestCompositionSwitchFailureRestoresOldDurableBinding(t *testing.T) {
	for _, failure := range []string{"build", "persist-after-write"} {
		t.Run(failure, func(t *testing.T) {
			s, old := compositionSwitchFixture(t)
			original := old.CompositionBinding().Digest
			if failure == "build" {
				s.compositionAssembly.build = func(context.Context, boot.Options) (*boot.BuildResult, error) { return nil, errors.New("build failed") }
			} else {
				s.compositionAssembly.persist = func(next *control.Controller) error {
					if err := next.SnapshotRewrite(); err != nil {
						return err
					}
					return errors.New("sidecar write failed")
				}
			}
			if err := old.SelectAgentComposition(t.Context(), "user:alternate"); err == nil {
				t.Fatal("failure hidden")
			}
			if s.ctl() != old || old.CompositionBinding().Digest != original {
				t.Fatal("failed switch replaced old session")
			}
			loaded, err := sessionstore.LoadSession(old.SessionPath())
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Composition().Digest != original {
				t.Fatal("failed publication changed durable binding")
			}
			if err := old.SelectAgentComposition(t.Context(), "builtin:minimal"); err != nil {
				t.Fatalf("old session unusable: %v", err)
			}
		})
	}
}

func TestCompositionNewSessionIsUnboundDespiteStoredDefault(t *testing.T) {
	s, old := compositionSwitchFixture(t)
	if err := old.SetDefaultAgentPreset("user:alternate", true); err != nil {
		t.Fatal(err)
	}
	if err := old.NewSession(); err != nil {
		t.Fatal(err)
	}
	next := s.ctl().(*control.Controller)
	if next == old || next.CompositionBinding() != nil || next.SessionPath() == old.SessionPath() {
		t.Fatal("new session reused old binding or path")
	}
}
