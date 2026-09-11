package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/sessioncatalog"
)

func TestRebuildSessionCatalogDoesNotReplaceBeforeOldReconcileStops(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.startSessionCatalog()
	oldCatalog := waitForSessionCatalogForTest(t, app, nil)
	t.Cleanup(func() { app.stopSessionCatalog(time.Second) })

	catalogPath := sessioncatalog.DefaultPath()
	before, err := os.Stat(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	app.catalogReconcileHook = func(sessioncatalog.DirectoryTarget) {
		close(started)
		<-release
	}
	app.catalogReconcileDoneHook = func(sessioncatalog.DirectoryTarget) { close(finished) }
	if !app.requestSessionCatalogReconcile(dir) {
		t.Fatal("explicit reconcile was not scheduled")
	}
	<-started

	stopTimeout := 25 * time.Millisecond
	err = app.rebuildSessionCatalog(stopTimeout)
	if !errors.Is(err, errSessionCatalogStopTimeout) {
		close(release)
		t.Fatalf("rebuild error = %v, want stop deadline error", err)
	}
	if app.catalogRebuilding.Load() {
		close(release)
		t.Fatal("timed-out rebuild left rebuilding set")
	}
	after, err := os.Stat(catalogPath)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		close(release)
		t.Fatal("rebuild replaced the live projection before the old reconcile stopped")
	}
	if backups, err := filepath.Glob(catalogPath + ".replaced-*"); err != nil || len(backups) != 0 {
		close(release)
		t.Fatalf("replacement backups after aborted rebuild = %v, err = %v", backups, err)
	}

	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("old reconcile did not finish after release")
	}
	assertSessionCatalogWatcherRunning(t, app)
	_ = waitForSessionCatalogForTest(t, app, oldCatalog)
}
