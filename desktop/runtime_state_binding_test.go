package main

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

// The gate pauses a real projection read after App bindings were copied. Its
// result may then belong to a controller whose session was rotated meanwhile.
type bindingRuntimeReader struct {
	stubSessionAPI
	mu       sync.Mutex
	state    event.RuntimeStateSnapshot
	entered  chan struct{}
	release  chan struct{}
	once     sync.Once
	resolved []control.PromptIdentity
}

func (*bindingRuntimeReader) AutoApproveTools() bool { return false }
func (*bindingRuntimeReader) PlanMode() bool         { return false }
func (*bindingRuntimeReader) Goal() string           { return "" }
func (*bindingRuntimeReader) GoalStatus() string     { return control.GoalStatusStopped }
func (*bindingRuntimeReader) GoalRuntime() control.GoalRuntimeView {
	return control.GoalRuntimeView{}
}
func (*bindingRuntimeReader) ToolApprovalMode() string { return "ask" }

func (r *bindingRuntimeReader) ResolvePromptExact(identity control.PromptIdentity, _ control.PromptAnswer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resolved = append(r.resolved, identity)
	return nil
}

func (r *bindingRuntimeReader) RuntimeStateSnapshot() event.RuntimeStateSnapshot {
	r.once.Do(func() {
		if r.entered != nil {
			close(r.entered)
			<-r.release
		}
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

func TestRuntimeStateProjectionDoesNotHoldMutexAcrossControllerRead(t *testing.T) {
	reader := &bindingRuntimeReader{
		state:   event.RuntimeStateSnapshot{SchemaVersion: 1, Phase: "executing", Running: true},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	tab := &WorkspaceTab{ID: "running", Scope: "global", TopicID: "topic", SessionPath: "/run.jsonl", Ctrl: reader}
	app := &App{tabs: map[string]*WorkspaceTab{tab.ID: tab}, detachedSessions: map[string]*WorkspaceTab{}}
	done := make(chan RuntimeStateProjection, 1)
	go func() { done <- app.GetRuntimeStateSnapshot() }()
	select {
	case <-reader.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("projection did not reach controller read")
	}
	if !app.runtimeStateProjection.mu.TryLock() {
		close(reader.release)
		t.Fatal("GetRuntimeStateSnapshot held its projection mutex across a controller read")
	}
	app.runtimeStateProjection.mu.Unlock()
	close(reader.release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("gated projection did not finish")
	}
}

func TestMetaForTabRevalidatesBindingAndReadsControllerUnlocked(t *testing.T) {
	old := &bindingRuntimeReader{
		state: event.RuntimeStateSnapshot{SchemaVersion: 1, ProjectionEpoch: "old-producer", RuntimeEpoch: "old-runtime", Revision: 5,
			Phase: "executing", Running: true, Todos: []event.Todo{{Content: "old", Status: "in_progress"}}},
		entered: make(chan struct{}), release: make(chan struct{}),
	}
	newState := event.RuntimeStateSnapshot{SchemaVersion: 1, ProjectionEpoch: "new-producer", RuntimeEpoch: "new-runtime", Revision: 1,
		Phase: "idle", Todos: []event.Todo{{Content: "new", Status: "pending"}}}
	next := &bindingRuntimeReader{state: newState}
	tab := &WorkspaceTab{ID: "meta-binding", Scope: "global", WorkspaceRoot: "/tmp/meta-binding", SessionPath: "/old.jsonl", SessionID: "old-session", SessionGeneration: 1, Ctrl: old, Ready: true}
	// Keep the test focused on the runtime sample. A cache miss schedules an
	// unrelated metadata refresh which briefly takes App.mu and can make the
	// lock assertion nondeterministic under the race detector.
	tab.metaExtras.Store(&tabMetaExtras{controller: old, workspaceRoot: tab.WorkspaceRoot, fetchedAt: time.Now()})
	a := &App{tabs: map[string]*WorkspaceTab{tab.ID: tab}, detachedSessions: map[string]*WorkspaceTab{}}
	done := make(chan Meta, 1)
	go func() { done <- a.MetaForTab(tab.ID) }()
	select {
	case <-old.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("MetaForTab did not reach controller snapshot")
	}
	if !a.mu.TryLock() {
		close(old.release)
		t.Fatal("MetaForTab held App.mu while reading the controller")
	}
	tab.Ctrl = next
	tab.SessionID = "new-session"
	tab.SessionPath = "/new.jsonl"
	tab.SessionGeneration = 2
	a.mu.Unlock()
	close(old.release)
	select {
	case got := <-done:
		if got.SessionID != "new-session" || got.SessionGeneration != 2 || got.RuntimeStateSnapshot == nil ||
			!reflect.DeepEqual(*got.RuntimeStateSnapshot, newState) || got.CanonicalTodos == nil || len(*got.CanonicalTodos) != 1 || (*got.CanonicalTodos)[0].Content != "new" {
			t.Fatalf("MetaForTab paired stale identity and state: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("MetaForTab did not finish after controller replacement")
	}
}

func TestResolvePromptForSessionRejectsStaleBindingBeforeController(t *testing.T) {
	reader := &bindingRuntimeReader{}
	tab := &WorkspaceTab{ID: "prompt", SessionID: "session-a", SessionGeneration: 3, Ctrl: reader}
	a := &App{tabs: map[string]*WorkspaceTab{tab.ID: tab}}
	target := InteractionTargetView{TabID: tab.ID, HostID: localDesktopHostID, SessionID: tab.SessionID,
		SessionGeneration: 2, PromptID: "p1", TurnID: "t1", RuntimeEpoch: "r1", Kind: "ask"}
	if err := a.ResolvePromptForSession(target, PromptAnswerView{}); err == nil {
		t.Fatal("stale session generation reached prompt resolver")
	}
	reader.mu.Lock()
	if len(reader.resolved) != 0 {
		t.Fatalf("stale target called controller: %+v", reader.resolved)
	}
	reader.mu.Unlock()
	target.SessionGeneration = 3
	if err := a.ResolvePromptForSession(target, PromptAnswerView{}); err != nil {
		t.Fatalf("current target rejected: %v", err)
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if len(reader.resolved) != 1 || reader.resolved[0].PromptID != "p1" {
		t.Fatalf("current target calls = %+v", reader.resolved)
	}
}

func TestResolvePromptForSessionAcceptsInitialGeneration(t *testing.T) {
	reader := &bindingRuntimeReader{}
	// New and restored tabs start at generation zero until a rotation occurs.
	tab := &WorkspaceTab{ID: "initial", SessionID: "session-a", Ctrl: reader}
	a := &App{tabs: map[string]*WorkspaceTab{tab.ID: tab}}
	target := InteractionTargetView{TabID: tab.ID, HostID: localDesktopHostID,
		SessionID: tab.SessionID, SessionGeneration: tab.SessionGeneration,
		PromptID: "p1", TurnID: "t1", RuntimeEpoch: "r1", Kind: "approval"}
	if err := a.ResolvePromptForSession(target, PromptAnswerView{Allow: true}); err != nil {
		t.Fatalf("initial generation rejected: %v", err)
	}
	// A delayed answer from generation zero must not authorize the rotated tab.
	tab.SessionGeneration++
	if err := a.ResolvePromptForSession(target, PromptAnswerView{Allow: true}); err == nil {
		t.Fatal("initial generation authorized a rotated session")
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if len(reader.resolved) != 1 || reader.resolved[0].PromptID != target.PromptID {
		t.Fatalf("initial prompt calls = %+v", reader.resolved)
	}
}

func TestLocalBindingUsesControllerIdentityNotMutableContents(t *testing.T) {
	first, second := &bindingRuntimeReader{}, &bindingRuntimeReader{}
	tab := &WorkspaceTab{ID: "identity"}
	sampled := localRuntimeBinding{tab: tab, ctrl: first}
	current := sampled
	current.ctrl = second
	if sameLocalRuntimeBinding(current, sampled) {
		t.Fatal("different controller instances were treated as the same binding")
	}
	first.mu.Lock()
	defer first.mu.Unlock()
	if !sameLocalRuntimeBinding(sampled, sampled) {
		t.Fatal("controller's mutable lock state changed its binding identity")
	}
}

func TestRuntimeStateProjectionRevalidatesLocalBindingAfterSampling(t *testing.T) {
	for _, mutation := range []string{"controller", "generation", "path", "scope", "tab", "detach", "close"} {
		t.Run(mutation, func(t *testing.T) {
			old := &bindingRuntimeReader{state: event.RuntimeStateSnapshot{SchemaVersion: 1, RuntimeEpoch: "old", Revision: 1, Phase: "executing", Running: true},
				entered: make(chan struct{}), release: make(chan struct{})}
			nextState := event.RuntimeStateSnapshot{SchemaVersion: 1, RuntimeEpoch: "new", Revision: 2, Phase: "idle"}
			next := &bindingRuntimeReader{state: nextState}
			tab := &WorkspaceTab{ID: "binding", Scope: "global", SessionPath: "/old.jsonl", SessionGeneration: 1, Ctrl: old}
			a := &App{tabs: map[string]*WorkspaceTab{tab.ID: tab}, detachedSessions: map[string]*WorkspaceTab{}}
			done := make(chan RuntimeStateProjection, 1)
			go func() { done <- a.GetRuntimeStateSnapshot() }()
			select {
			case <-old.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("projection did not reach controller read")
			}
			// Acquiring App.mu here is also the deterministic proof that the
			// runtime reader never runs while holding the application lock.
			a.mu.Lock()
			switch mutation {
			case "controller":
				tab.Ctrl = next
			case "generation":
				tab.SessionGeneration++
			case "path":
				tab.SessionPath = "/new.jsonl"
			case "scope":
				tab.Scope, tab.WorkspaceRoot = "project", "/workspace"
			case "tab":
				tab = &WorkspaceTab{ID: tab.ID, Scope: "global", SessionPath: "/new.jsonl", SessionGeneration: 2, Ctrl: next}
				a.tabs[tab.ID] = tab
			case "detach":
				delete(a.tabs, tab.ID)
				a.detachedSessions[tab.SessionPath] = tab
			case "close":
				delete(a.tabs, tab.ID)
			}
			old.mu.Lock()
			if mutation != "controller" && mutation != "tab" {
				old.state = nextState
			}
			old.mu.Unlock()
			wantPath, wantGeneration := tab.SessionPath, tab.SessionGeneration
			wantScope, wantRoot := tab.Scope, tab.WorkspaceRoot
			a.mu.Unlock()
			close(old.release)
			var got RuntimeStateProjection
			select {
			case got = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("projection did not finish after binding replacement")
			}
			if mutation == "close" {
				if len(got.Sessions) != 0 {
					t.Fatalf("closed binding leaked into projection: %+v", got.Sessions)
				}
				return
			}
			if len(got.Sessions) != 1 {
				t.Fatalf("expected one current binding: %+v", got.Sessions)
			}
			view := got.Sessions[0]
			if view.SessionPath != wantPath || view.SessionGeneration != wantGeneration || view.Scope != wantScope || view.WorkspaceRoot != wantRoot || !reflect.DeepEqual(view.State, nextState) || view.Open != (mutation != "detach") {
				t.Fatalf("projection paired state with stale binding: %+v", view)
			}
			if fresh := a.GetRuntimeStateSnapshot(); fresh.Revision != got.Revision {
				t.Fatalf("binding repair required an unrelated subsequent read: first=%+v fresh=%+v", got, fresh)
			}
		})
	}
}
