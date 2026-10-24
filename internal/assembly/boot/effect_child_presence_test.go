package boot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/executionpresence"
)

func TestEffectIsolatedChildRegistersItsActualWorkingCopy(t *testing.T) {
	home, root := statedBootHome(t), testenv.TempDir(t)
	p := &presenceWaitingProvider{entered: make(chan struct{}), leave: make(chan struct{})}
	provider.Register("boot-child-presence", func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, root, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "BASE"
collaboration = "shared"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "boot-child-presence"
model = "x"
`)
	mirrorToUserConfigAt(t, home, root)
	gitInDir(t, root, "init", "-q")
	gitInDir(t, root, "add", "-A")
	gitInDir(t, root, "commit", "-qm", "base")
	b := newBuilder()
	built, err := b.build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer built.Controller.Close()
	bindPresenceSession(t, built.Controller)
	ctx := agent.WithParentSession(t.Context(), "presence-owner")
	done := make(chan error, 1)
	finished := make(chan struct{})
	var leaveOnce sync.Once
	defer func() { leaveOnce.Do(func() { close(p.leave) }); <-finished }()
	go func() {
		defer close(finished)
		_, err := b.tools.taskTool.Execute(ctx, json.RawMessage(`{"prompt":"think","isolation":"worktree"}`))
		done <- err
	}()
	select {
	case <-p.entered:
	case err := <-done:
		t.Fatalf("child exited before model: %v", err)
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	markers, err := filepath.Glob(filepath.Join(config.RootsForHome(home).DeliveryWorktreeDir(), "isolated", "*", "entry-v2.json"))
	if err != nil || len(markers) != 1 {
		t.Fatalf("candidate markers: %v %v", markers, err)
	}
	raw, err := os.ReadFile(markers[0])
	if err != nil {
		t.Fatal(err)
	}
	var entry struct {
		Cand worktree.Candidate `json:"cand"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []struct {
		root     string
		selected executionpresence.Strategy
	}{{root, executionpresence.Shared}, {entry.Cand.WorktreeRoot, executionpresence.Isolated}} {
		store, err := executionpresence.New(scope.root, config.WorkspaceLeaseDir())
		if err != nil {
			t.Fatal(err)
		}
		reservation, err := store.Reserve(t.Context(), executionpresence.Request{Strategy: executionpresence.Auto, Writer: true, SessionID: "other-chat"})
		if err != nil {
			t.Fatal(err)
		}
		got := reservation.Record().Selected
		reservation.Close()
		if got != scope.selected {
			t.Fatalf("active child at %s: selected %s, want %s", scope.root, got, scope.selected)
		}
	}
	leaveOnce.Do(func() { close(p.leave) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
