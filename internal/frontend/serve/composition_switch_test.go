package serve

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"reasonix/internal/assembly/boot"
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
	home, root, dir := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	build := func(_ context.Context, opts boot.Options) (*boot.BuildResult, error) {
		store := agentpreset.New(home, root)
		id := opts.AgentComposition
		if id == "default" {
			id, _ = store.Default()
		}
		v, err := store.Get(id)
		if err != nil {
			return nil, err
		}
		binding := &agentcomposition.Binding{Version: v, Resolved: v.Definition, Workspace: root}
		binding.Seal()
		session := sessionstore.NewSession(v.Definition.Name)
		session.BindComposition(binding)
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
	if err := old.SelectAgentComposition(t.Context(), "builtin:general"); err != nil {
		t.Fatal(err)
	}
	next := s.ctl().(*control.Controller)
	if next == old || next.CompositionBinding().Version.Definition.ID != "builtin:general" {
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
	if err := next.SelectAgentComposition(t.Context(), "builtin:general"); err != nil {
		t.Fatal(err)
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
			if err := old.SelectAgentComposition(t.Context(), "builtin:general"); err == nil {
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

func TestCompositionNewSessionResolvesCurrentWorkspaceDefault(t *testing.T) {
	s, old := compositionSwitchFixture(t)
	if err := old.SetDefaultAgentPreset("builtin:general", true); err != nil {
		t.Fatal(err)
	}
	if err := old.NewSession(); err != nil {
		t.Fatal(err)
	}
	next := s.ctl().(*control.Controller)
	if next == old || next.CompositionBinding().Version.Definition.ID != "builtin:general" || next.SessionPath() == old.SessionPath() {
		t.Fatal("new session reused old binding or path")
	}
}
