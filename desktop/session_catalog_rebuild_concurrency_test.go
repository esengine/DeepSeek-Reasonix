package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/sessioncatalog"
)

func TestConcurrentRebuildSessionCatalogCallersShareFailure(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.startSessionCatalog()
	_ = waitForSessionCatalogForTest(t, app, nil)
	t.Cleanup(func() { app.stopSessionCatalog(time.Second) })

	projectRoot := filepath.Join(t.TempDir(), "broken-project")
	if err := addProject(projectRoot, "Broken project"); err != nil {
		t.Fatal(err)
	}
	badTarget := desktopSessionDir(projectRoot)
	if err := os.MkdirAll(filepath.Dir(badTarget), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badTarget, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	reconcileStarted := make(chan struct{})
	reconcileRelease := make(chan struct{})
	released := false
	t.Cleanup(func() {
		if !released {
			close(reconcileRelease)
		}
	})
	app.catalogReconcileHook = func(sessioncatalog.DirectoryTarget) {
		close(reconcileStarted)
		<-reconcileRelease
	}
	if !app.requestSessionCatalogReconcile(dir) {
		t.Fatal("explicit reconcile was not scheduled")
	}
	<-reconcileStarted

	app.ctx = context.Background()
	rebuildStarted := make(chan struct{})
	app.runtimeEvents.emit = func(_ context.Context, name string, payload ...any) {
		if name != "project-tree:changed-v2" || len(payload) != 1 {
			return
		}
		event, ok := payload[0].(ProjectTreeChangedV2)
		if ok && event.Reason == "catalog_rebuild_started" {
			close(rebuildStarted)
		}
	}
	joined := make(chan struct{})
	app.catalogRebuildJoinHook = func() { close(joined) }

	leaderDone := make(chan error, 1)
	go func() { leaderDone <- app.rebuildSessionCatalog(3 * time.Second) }()
	<-rebuildStarted
	followerDone := make(chan error, 1)
	go func() { followerDone <- app.rebuildSessionCatalog(3 * time.Second) }()
	<-joined
	select {
	case err := <-followerDone:
		t.Fatalf("concurrent rebuild returned before the owner completed: %v", err)
	default:
	}

	close(reconcileRelease)
	released = true
	leaderErr := <-leaderDone
	followerErr := <-followerDone
	if leaderErr == nil || followerErr == nil {
		t.Fatalf("concurrent rebuild errors = leader %v, follower %v; want shared failure", leaderErr, followerErr)
	}
	if followerErr.Error() != leaderErr.Error() || !strings.Contains(leaderErr.Error(), "not a directory") {
		t.Fatalf("concurrent rebuild errors = leader %q, follower %q; want the same rebuild result",
			leaderErr, followerErr)
	}
	if app.catalogRebuilding.Load() {
		t.Fatal("shared failed rebuild left rebuilding set")
	}
}
