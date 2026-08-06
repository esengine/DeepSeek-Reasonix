package navigator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestObserverModeBaseline covers the happy path of the observer-mode pair:
// BeginAction predicts, EndAction observes the unchanged environment and
// returns StrategyContinue with the state graph advanced by one step.
func TestObserverModeBaseline(t *testing.T) {
	ctx := context.Background()
	adapter := &mockAdapter{permAllow: true}
	n := New(adapter, Options{HistoryWindow: 20})
	adapter.outputs = []string{"read ok"}

	if _, err := n.BeginAction(ctx, HostAction{Verb: "read", Target: "/app/config.yaml", Args: `{}`}); err != nil {
		t.Fatalf("BeginAction: %v", err)
	}
	corr, err := n.EndAction(ctx, HostAction{Verb: "read", Target: "/app/config.yaml", Args: `{}`}, HostResult{Output: "read ok"})
	if err != nil {
		t.Fatalf("EndAction: %v", err)
	}
	if corr.Strategy != StrategyContinue {
		t.Errorf("expected StrategyContinue, got %v: %s", corr.Strategy, corr.Reason)
	}
	latest, ok := n.StateManager().History().Latest()
	if !ok || latest.Step != 1 {
		t.Errorf("expected history at step 1, got step %d (ok=%v)", latest.Step, ok)
	}
}

// TestObserverModeRecoversFacts verifies that the implicit-fact recovery runs
// on the observer path too — the same defense the Execute path provides.
func TestObserverModeRecoversFacts(t *testing.T) {
	ctx := context.Background()
	adapter := &mockAdapter{permAllow: true}
	n := New(adapter, Options{HistoryWindow: 20})

	if _, err := n.BeginAction(ctx, HostAction{Verb: "read", Args: `{}`}); err != nil {
		t.Fatal(err)
	}
	if _, err := n.EndAction(ctx, HostAction{Verb: "read", Args: `{}`}, HostResult{Output: "Result: /var/log/app.log has 42 entries"}); err != nil {
		t.Fatal(err)
	}
	digest := n.StateManager().ImplicitStateDigest()
	if !strings.Contains(digest, "/var/log/app.log") {
		t.Errorf("observer path should recover implicit facts, digest: %s", digest)
	}
}

// TestObserverModeDetectsEnvChange uses a real FilesystemSensor so EndAction
// sees a different environment digest than BeginAction predicted — the
// "dead light under the lamp" (dynamic environment) case. A read action must
// not touch the filesystem, so drift on EnvHash is a real deviation.
func TestObserverModeDetectsEnvChange(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	// Seed a non-empty baseline so the predicted env hash is non-empty.
	if err := os.WriteFile(filepath.Join(dir, "baseline.txt"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	adapter := &mockAdapter{permAllow: true}
	n := New(adapter, Options{HistoryWindow: 20})
	n.AddSensor(NewFilesystemSensor(dir, 3))

	if _, err := n.BeginAction(ctx, HostAction{Verb: "read", Args: `{}`}); err != nil {
		t.Fatal(err)
	}
	// The environment changes while the (hypothetical) read runs.
	if err := os.WriteFile(filepath.Join(dir, "drift.txt"), []byte("new file"), 0o644); err != nil {
		t.Fatal(err)
	}
	corr, err := n.EndAction(ctx, HostAction{Verb: "read", Args: `{}`}, HostResult{Output: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if corr.Strategy == StrategyContinue {
		t.Error("expected a deviation correction when the filesystem drifted, got continue")
	}
}

// TestObserverModePermissionDenied: BeginAction must fail closed at the
// permission gate, matching Execute's behavior.
func TestObserverModePermissionDenied(t *testing.T) {
	ctx := context.Background()
	adapter := &mockAdapter{permAllow: false, permReason: "deny rule"}
	n := New(adapter, Options{HistoryWindow: 20})

	_, err := n.BeginAction(ctx, HostAction{Verb: "write", Args: `{}`})
	if !errors.Is(err, ErrAskHost) {
		t.Errorf("expected ErrAskHost, got %v", err)
	}
}

// TestObserverModeMatchesExecute asserts the composed Execute and the
// observer-mode pair leave the state manager in an equivalent state (same
// step count, same recovered facts).
func TestObserverModeMatchesExecute(t *testing.T) {
	ctx := context.Background()

	// Path 1: composed Execute.
	adapter1 := &mockAdapter{permAllow: true}
	n1 := New(adapter1, Options{HistoryWindow: 20})
	adapter1.outputs = []string{"path: /tmp/a.txt"}
	if _, _, err := n1.Execute(ctx, HostAction{Verb: "read", Args: `{}`}); err != nil {
		t.Fatal(err)
	}

	// Path 2: BeginAction → host dispatches (mocked here) → EndAction.
	adapter2 := &mockAdapter{permAllow: true}
	n2 := New(adapter2, Options{HistoryWindow: 20})
	if _, err := n2.BeginAction(ctx, HostAction{Verb: "read", Args: `{}`}); err != nil {
		t.Fatal(err)
	}
	if _, err := n2.EndAction(ctx, HostAction{Verb: "read", Args: `{}`}, HostResult{Output: "path: /tmp/a.txt"}); err != nil {
		t.Fatal(err)
	}

	s1, _ := n1.StateManager().History().Latest()
	s2, _ := n2.StateManager().History().Latest()
	if s1.Step != s2.Step {
		t.Errorf("step mismatch: Execute=%d observer=%d", s1.Step, s2.Step)
	}
	d1 := n1.StateManager().ImplicitStateDigest()
	d2 := n2.StateManager().ImplicitStateDigest()
	if d1 != d2 {
		t.Errorf("digest mismatch:\n Execute:  %s\n Observer: %s", d1, d2)
	}
}

// TestObserverConcurrentWithDigest runs Begin/End concurrently with digest
// reads to exercise the manager's own locking (the observer path leaves the
// window between Begin and End unlocked for background sensors).
func TestObserverConcurrentWithDigest(t *testing.T) {
	ctx := context.Background()
	adapter := &mockAdapter{permAllow: true}
	n := New(adapter, Options{HistoryWindow: 100})
	adapter.outputs = []string{"ok"}

	// Seed and reach a known state before racing the reader goroutine.
	if _, err := n.BeginAction(ctx, HostAction{Verb: "read", Args: `{}`}); err != nil {
		t.Fatal(err)
	}
	if _, err := n.EndAction(ctx, HostAction{Verb: "read", Args: `{}`}, HostResult{Output: "ok"}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			if _, err := n.BeginAction(ctx, HostAction{Verb: "read", Args: `{}`}); err != nil {
				t.Error(err)
				return
			}
			if _, err := n.EndAction(ctx, HostAction{Verb: "read", Args: `{}`}, HostResult{Output: "ok"}); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = n.StateManager().ImplicitStateDigest()
			if _, ok := n.StateManager().History().Latest(); !ok {
				t.Error("history empty")
				return
			}
		}
	}()
	wg.Wait()
}
