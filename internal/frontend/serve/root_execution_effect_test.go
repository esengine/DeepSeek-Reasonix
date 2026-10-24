package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

func TestSubmitAutoIsolationPublishesBeforeCapturingTurnRuntime(t *testing.T) {
	h := recoveryHarness(t)
	h.model.mode = "block"
	path := config.RootsForHome(h.home).UserConfigPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), `collaboration = "shared"`, `collaboration = "auto"`, 1))
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.RootsForHome(h.home).LoadForRootReadOnly(h.root)
	if err != nil {
		t.Fatal(err)
	}
	canIsolate := sandbox.AvailableFor(sandbox.Spec{Mode: "enforce", RequiredIntegrity: sandbox.CompleteFileIntegrity, Network: cfg.Sandbox.Network})
	t.Logf("automatic program isolation available: %t", canIsolate)
	firstHub, first, firstBC := h.pane(filepath.Join(SessionDirFor(h.root), "first.jsonl"))
	defer firstHub.Shutdown()
	secondHub, second, secondBC := h.pane(filepath.Join(SessionDirFor(h.root), "second.jsonl"))
	defer secondHub.Shutdown()
	firstFrames, unsubscribeFirst := firstBC.Subscribe()
	defer unsubscribeFirst()
	secondFrames, unsubscribeSecond := secondBC.Subscribe()
	defer unsubscribeSecond()
	old := second.Server.Controller()
	submit := func(server *Server, input string) {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/submit", strings.NewReader(`{"input":"`+input+`"}`))
		rec := httptest.NewRecorder()
		server.submit(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("submit = %d %s", rec.Code, rec.Body.String())
		}
	}
	submit(first.Server, "first task")
	select {
	case <-h.model.started:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	submit(second.Server, "second task")
	next, ok := second.Server.Controller().(*control.Controller)
	if !ok {
		t.Fatal("submit lost the concrete turn controller")
	}
	if !canIsolate {
		if next != old || next.RootEnvironment() != nil || next.WorkspaceRepo().WorkTree != first.Server.Controller().(*control.Controller).WorkspaceRepo().WorkTree {
			t.Fatal("unavailable automatic isolation did not retain the source runtime")
		}
		if binding, bound, err := sessionstore.LoadExecutionEnvironment(next.SessionPath()); err != nil || bound {
			t.Fatalf("fallback published an unusable environment: %+v %v %v", binding, bound, err)
		}
	} else {
		assertIsolatedSubmitRuntime(t, first.Server.Controller().(*control.Controller), old, next)
	}
	first.Server.Controller().Cancel()
	next.Cancel()
	for _, frames := range []<-chan Frame{firstFrames, secondFrames} {
		for {
			select {
			case frame := <-frames:
				var event struct {
					Kind string `json:"kind"`
				}
				if json.Unmarshal(frame.Data, &event) == nil && event.Kind == "turn_done" {
					goto finished
				}
			case <-t.Context().Done():
				t.Fatal(t.Context().Err())
			}
		}
	finished:
	}
	waitNotRunning(t, next)
	if canIsolate {
		assertIsolatedSessionRotation(t, next)
	}
}

func assertIsolatedSubmitRuntime(t *testing.T, first *control.Controller, old control.SessionAPI, next *control.Controller) {
	t.Helper()
	if next == old || next.RootEnvironment() == nil {
		t.Fatal("submit retained source controller")
	}
	if next.WorkspaceRepo().WorkTree == first.WorkspaceRepo().WorkTree {
		t.Fatal("parallel writable chats share the source copy")
	}
	if next.SessionPath() != old.SessionPath() {
		t.Fatal("automatic isolation forked the conversation")
	}
	binding, bound, err := sessionstore.LoadExecutionEnvironment(next.SessionPath())
	if err != nil || !bound || binding.EnvironmentID != next.RootEnvironment().Record().ID {
		t.Fatalf("session binding = %+v %v %v", binding, bound, err)
	}
	if err := next.CheckSessionEnvironment(next.SessionPath()); err != nil {
		t.Fatal(err)
	}
	if old.Running() {
		t.Fatal("source captured the delegated turn")
	}
	for _, message := range old.History() {
		if message.Content == "second task" {
			t.Fatal("input appended to source history")
		}
	}
}

func assertIsolatedSessionRotation(t *testing.T, next *control.Controller) {
	t.Helper()
	initialPath := next.SessionPath()
	for _, action := range []struct {
		name string
		run  func() error
	}{
		{"new", next.NewSession}, {"clear", next.ClearSession},
	} {
		before := next.SessionPath()
		if err := action.run(); err != nil {
			t.Fatalf("%s: %v", action.name, err)
		}
		if next.SessionPath() == before {
			t.Fatal("session path was not rotated")
		}
		if err := next.CheckSessionEnvironment(next.SessionPath()); err != nil {
			t.Fatalf("%s environment: %v", action.name, err)
		}
		if !next.Executor().Session().WriteAuthority().Covers(next.SessionPath()) {
			t.Fatalf("%s lost transcript authority", action.name)
		}
		next.Executor().Session().Add(provider.Message{Role: provider.RoleUser, Content: action.name + " isolated conversation"})
		if err := next.Snapshot(); err != nil {
			t.Fatalf("%s persistence: %v", action.name, err)
		}
	}
	if err := next.CheckSessionEnvironment(initialPath); err != nil {
		t.Fatal("rotation invalidated retained original attachment", err)
	}
	before := next.SessionPath()
	blockedArtifact := store.SessionWireLog(before)
	if err := os.MkdirAll(filepath.Join(blockedArtifact, "not-a-file"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := next.ClearSession(); err != nil {
		t.Fatal("published clear returned a cleanup failure", err)
	}
	if next.SessionPath() == before || !next.Executor().Session().WriteAuthority().Covers(next.SessionPath()) {
		t.Fatal("cleanup failure stranded old path or lost new authority")
	}
	if !sessionstore.IsCleanupPending(before) {
		t.Fatal("failed cleanup lost durable retry intent")
	}
	if err := next.CheckSessionEnvironment(next.SessionPath()); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(blockedArtifact); err != nil {
		t.Fatal(err)
	}
	if err := control.ReconcileCleanupPending(next.SessionDir()); err != nil {
		t.Fatal(err)
	}
	if sessionstore.IsCleanupPending(before) {
		t.Fatal("cleanup did not recover")
	}
}
