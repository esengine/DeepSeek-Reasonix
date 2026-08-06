package navigator

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestBackgroundWatchDetectsEnvChangeOutsideActions verifies the "dead light"
// defense: a file created while no tool call runs is still sampled by the
// background watch and becomes a correlated event the host flushes at the
// next EndAction.
func TestBackgroundWatchDetectsEnvChangeOutsideActions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "baseline.txt"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	adapter := &mockAdapter{permAllow: true}
	n := New(adapter, Options{HistoryWindow: 20})
	n.AddSensor(NewFilesystemSensor(dir, 3))
	n.StartBackgroundWatch(ctx, 20*time.Millisecond)

	// BeginAction seeds the baseline environment (before the background
	// change), so the root snapshot reflects the pre-change state.
	if _, err := n.BeginAction(ctx, HostAction{Verb: "read", Args: `{}`}); err != nil {
		t.Fatal(err)
	}
	root, _ := n.StateManager().History().Latest()

	// A change happens outside any tool call (e.g. a download finishing).
	time.Sleep(60 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(dir, "background_download.tmp"), []byte("done"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)

	// The next tool call's EndAction flushes the correlated events the watch
	// accumulated and records the drifted env hash.
	if _, err := n.EndAction(ctx, HostAction{Verb: "read", Args: `{}`}, HostResult{Output: "ok"}); err != nil {
		t.Fatal(err)
	}

	latest, ok := n.StateManager().History().Latest()
	if !ok {
		t.Fatal("no state recorded")
	}
	if latest.EnvHash == "" {
		t.Error("expected a non-empty env hash after background change")
	}
	if latest.EnvHash == root.EnvHash {
		t.Error("env hash should have drifted after a background file appeared")
	}
}

// TestBackgroundWatchIdempotent: a second StartBackgroundWatch call must not
// panic or double-start (guarded by watchStarted).
func TestBackgroundWatchIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	n := New(&mockAdapter{permAllow: true}, Options{})
	n.StartBackgroundWatch(ctx, 10*time.Millisecond)
	n.StartBackgroundWatch(ctx, 10*time.Millisecond) // no-op
	time.Sleep(30 * time.Millisecond)
}

// TestBackgroundWatchStopsOnCancel: the watcher must exit when ctx is
// cancelled so long-lived hosts don't leak goroutines.
func TestBackgroundWatchStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	n := New(&mockAdapter{permAllow: true}, Options{})
	n.StartBackgroundWatch(ctx, 5*time.Millisecond)
	cancel()
	// Nothing to assert beyond "does not panic"; the goroutine exits via
	// ctx.Done. Give it a moment under -race to catch data races on exit.
	time.Sleep(20 * time.Millisecond)
}
