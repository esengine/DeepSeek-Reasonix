package main

import (
	"sync"
	"testing"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

// The gate pauses a real projection read after App bindings were copied. Its
// result may then belong to a controller whose session was rotated meanwhile.
type bindingRuntimeReader struct {
	control.SessionAPI
	mu      sync.Mutex
	state   event.RuntimeStateSnapshot
	entered chan struct{}
	release chan struct{}
	once    sync.Once
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
			if view.SessionPath != wantPath || view.SessionGeneration != wantGeneration || view.Scope != wantScope || view.WorkspaceRoot != wantRoot || view.State != nextState || view.Open != (mutation != "detach") {
				t.Fatalf("projection paired state with stale binding: %+v", view)
			}
			if fresh := a.GetRuntimeStateSnapshot(); fresh.Revision != got.Revision {
				t.Fatalf("binding repair required an unrelated subsequent read: first=%+v fresh=%+v", got, fresh)
			}
		})
	}
}
