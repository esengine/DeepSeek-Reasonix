package serve

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

func TestSessionSweepDoesNotHoldTheBuild(t *testing.T) {
	release := make(chan struct{})
	started := make(chan string, 4)
	prev := sweepSessionDir
	sweepSessionDir = func(dir string) error {
		started <- dir
		<-release
		return nil
	}
	t.Cleanup(func() { sweepSessionDir = prev })

	dir := t.TempDir()
	returned := make(chan struct{})
	go func() {
		_ = BackgroundCleanupReconciler(dir)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("the reconciler held the caller while it swept the session directory")
	}
	select {
	case got := <-started:
		if got != dir {
			t.Fatalf("swept %q, want %q", got, dir)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the sweep never ran")
	}
	close(release)
}

func TestSessionSweepRunsOncePerDirectoryAtATime(t *testing.T) {
	release := make(chan struct{})
	var runs atomic.Int32
	started := make(chan struct{}, 4)
	prev := sweepSessionDir
	sweepSessionDir = func(string) error {
		runs.Add(1)
		started <- struct{}{}
		<-release
		return nil
	}
	t.Cleanup(func() { sweepSessionDir = prev })

	d1, d2 := t.TempDir(), t.TempDir()
	_ = BackgroundCleanupReconciler(d1)
	<-started
	_ = BackgroundCleanupReconciler(d1)
	_ = BackgroundCleanupReconciler(d2)
	<-started
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for runs.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if got := runs.Load(); got != 2 {
		t.Fatalf("ran %d sweeps, want 2 (one per distinct directory while in flight)", got)
	}
}

func TestEveryRuntimeBuildSweepsInTheBackground(t *testing.T) {
	s := &Server{}
	if s.rebuildOptions(nil, "").CleanupPendingReconciler == nil {
		t.Error("a model or effort rebuild sweeps session history inline")
	}
	if s.workspaceOptions(t.TempDir(), "").CleanupPendingReconciler == nil {
		t.Error("a workspace switch sweeps session history inline")
	}
}

func TestHubOpenSweepsItsSessionDirectoryInTheBackground(t *testing.T) {
	writeWalletConfig(t, "http://127.0.0.1:1")
	swept := make(chan string, 4)
	prev := sweepSessionDir
	sweepSessionDir = func(dir string) error {
		swept <- dir
		return nil
	}
	t.Cleanup(func() { sweepSessionDir = prev })

	h := NewHub(HubOptions{})
	defer h.Shutdown()
	root := testenv.TempDir(t)
	if _, err := h.Open(context.Background(), OpenRequest{Root: root}); err != nil {
		t.Fatal(err)
	}
	want := filepath.Clean(SessionDirFor(root))
	deadline := time.After(5 * time.Second)
	for {
		select {
		case got := <-swept:
			if filepath.Clean(got) == want {
				return
			}
		case <-deadline:
			t.Fatalf("Hub.Open never swept %s", want)
		}
	}
}
