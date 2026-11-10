package acp

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

type compositionFactory struct {
	home, dir string
	params    []SessionParams
	fail      error
}

func (f *compositionFactory) SessionDir() string { return f.dir }
func (f *compositionFactory) NewSession(_ context.Context, p SessionParams) (*control.Controller, error) {
	f.params = append(f.params, p)
	if f.fail != nil {
		return nil, f.fail
	}
	session := sessionstore.NewSession("pinned prompt")
	if p.Composition != nil {
		session.BindComposition(p.Composition)
	} else if p.AgentComposition != "" {
		d := agentcomposition.Builtins()[1]
		binding := &agentcomposition.Binding{Version: agentcomposition.Version{Definition: d, Revision: agentcomposition.Revision(d)}, Resolved: d, Workspace: p.Cwd, SystemPrompt: "pinned prompt"}
		binding.Seal()
		session.BindComposition(binding)
	}
	exec := agent.New(nil, tool.NewRegistry(), session, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, Runner: exec, Sink: event.Discard, WorkspaceRoot: p.Cwd, SessionDir: f.dir})
	ctrl.ConfigureCompositions(f.home, nil)
	return ctrl, nil
}

func TestCompositionACPNewIsUnboundAndLocksOnlyItsIndependentSelector(t *testing.T) {
	home, root := testenv.TempDir(t), testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	f := &compositionFactory{home: home}
	svc := &service{factory: f, sessions: map[string]*acpSession{}}
	raw, _ := json.Marshal(SessionNewParams{Cwd: root})
	if _, err := svc.sessionNew(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	if len(f.params) != 1 || f.params[0].AgentComposition != "" || f.params[0].Composition != nil {
		t.Fatal("new ACP session implicitly selected a composition")
	}
	for _, sess := range svc.sessions {
		ctrl := sess.currentCtrl().(*control.Controller)
		defer ctrl.Close()
		if ctrl.CompositionBinding() != nil {
			t.Fatal("new ACP session was bound")
		}
		history := sessionstore.NewSession("standard")
		history.Add(provider.Message{Role: provider.RoleUser, Content: "started"})
		if err := ctrl.Resume(history, ""); err != nil {
			t.Fatal(err)
		}
		state := withCompositionConfig(SessionConfigState{Model: "independent-model", ConfigOptions: []SessionConfigOption{{ID: "model"}}}, ctrl)
		if state.Model != "independent-model" || state.ConfigOptions[0].ID != "model" {
			t.Fatal("composition overwrote the model contract")
		}
		last := state.ConfigOptions[len(state.ConfigOptions)-1]
		if last.ID != "agent_composition" || len(last.Options) != 1 || last.CurrentValue != "builtin:general" {
			t.Fatalf("started selector was not locked: %+v", last)
		}
	}
}

func TestCompositionACPRestorePinsBindingAndKeepsUnavailableHistoryReadable(t *testing.T) {
	home, root, dir := testenv.TempDir(t), testenv.TempDir(t), testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	f := &compositionFactory{home: home, dir: dir}
	svc := &service{factory: f, sessions: map[string]*acpSession{}}
	original, err := f.NewSession(t.Context(), SessionParams{Cwd: root, AgentComposition: "default"})
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	path := filepath.Join(dir, "bound.jsonl")
	session := sessionstore.NewSession("pinned prompt")
	session.BindComposition(original.CompositionBinding())
	session.Add(provider.Message{Role: provider.RoleUser, Content: "historical request"})
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	restored, err := svc.newPersistedSession(t.Context(), SessionParams{Cwd: root, Model: "another-model"}, path)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	last := f.params[len(f.params)-1]
	if last.AgentComposition != "" || last.Composition.Digest != session.Composition().Digest || last.Model != "another-model" {
		t.Fatal("ACP restore followed the default or lost the model selection")
	}
	f.fail = errors.New("required module revoked")
	history, err := svc.newPersistedSession(t.Context(), SessionParams{Cwd: root}, path)
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	if err := history.Resume(session, path); err != nil {
		t.Fatal(err)
	}
	if history.CompositionExecutionError() == nil || len(history.History()) < 2 {
		t.Fatal("unavailable binding lost history or allowed execution")
	}
	f.fail = nil
	legacy := filepath.Join(dir, "legacy.jsonl")
	if err := sessionstore.NewSession("legacy prompt").Save(legacy); err != nil {
		t.Fatal(err)
	}
	old, err := svc.newPersistedSession(t.Context(), SessionParams{Cwd: root}, legacy)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if old.CompositionBinding() != nil || f.params[len(f.params)-1].AgentComposition != "" {
		t.Fatal("legacy ACP history was assigned a new default")
	}
}

func TestCompositionACPSwitchKeepsAdditionalDirectories(t *testing.T) {
	home, root, sibling := dirsWorld(t)
	f := &compositionFactory{home: home}
	old, err := f.NewSession(t.Context(), SessionParams{Cwd: root, AdditionalDirs: []string{sibling}})
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	sess := reloadExtensionsSession(t, "composition-dirs", old, &fakeNotifier{})
	sess.cwd = root
	sess.additionalDirs = []string{sibling}
	svc := &service{factory: f, sessions: map[string]*acpSession{sess.id: sess}}
	if err := svc.switchComposition(t.Context(), sess, old, "builtin:minimal"); err != nil {
		t.Fatal(err)
	}
	defer sess.currentCtrl().Close()
	got := f.params[len(f.params)-1]
	if len(got.AdditionalDirs) != 1 || got.AdditionalDirs[0] != sibling {
		t.Fatalf("composition switch lost the session's extra roots: %v", got.AdditionalDirs)
	}
	got.AdditionalDirs[0] = "changed candidate slice"
	if sess.additionalDirs[0] != sibling {
		t.Fatal("candidate shares the live session's extra-roots slice")
	}
}
