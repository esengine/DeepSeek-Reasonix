package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/sessioncatalog"
)

func TestStopSessionCatalogJoinsReconcileQueuedBeforeDiscoveryStarts(t *testing.T) {
	isolateDesktopUserDirs(t)
	ctx, cancel := context.WithCancel(t.Context())
	app := NewApp()
	app.ctx = ctx
	requested := make(chan struct{}, 1)
	catalog, err := sessioncatalog.Open(ctx, sessioncatalog.Options{
		InMemory: true, MetadataOnly: true, StartPaused: true,
		OnDiscovery: func(event sessioncatalog.DiscoveryEvent) {
			if event.Phase == "requested" {
				requested <- struct{}{}
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	app.sessionCatalog.Store(catalog)
	var jobDone <-chan struct{}
	t.Cleanup(func() {
		cancel()
		if !app.stopSessionCatalog(sessionCatalogTestDeadline) {
			t.Error("catalog fixture did not close cleanly")
		}
		if jobDone != nil && !waitChannelBefore(jobDone, time.Now().Add(sessionCatalogTestDeadline)) {
			t.Error("explicit reconcile outlived its fixture")
		}
	})

	dir := t.TempDir()
	if !app.requestSessionCatalogReconcile(dir) {
		t.Fatal("explicit reconcile was not admitted")
	}
	app.catalogReconcileMu.Lock()
	jobDone = app.catalogReconcileJobs[projectRootKey(dir)].done
	app.catalogReconcileMu.Unlock()
	// The worker is inside ScheduleReconcile, but paused discovery cannot settle
	// its token. Stop must cancel and join the desktop job independently.
	<-requested
	if !app.stopSessionCatalog(time.Second) {
		t.Fatal("catalog stop did not join its paused discovery waiter")
	}
	select {
	case <-jobDone:
	default:
		t.Fatal("catalog stop returned before its reconcile goroutine exited")
	}
}

func TestRetiredSessionCatalogReconcileCannotJoinReplacement(t *testing.T) {
	isolateDesktopUserDirs(t)
	ctx, cancel := context.WithCancel(t.Context())
	app := NewApp()
	app.ctx = ctx
	var oldRequests atomic.Int32
	openPaused := func(onDiscovery func(sessioncatalog.DiscoveryEvent)) *sessioncatalog.Catalog {
		catalog, err := sessioncatalog.Open(ctx, sessioncatalog.Options{
			InMemory: true, MetadataOnly: true, StartPaused: true, OnDiscovery: onDiscovery,
		})
		if err != nil {
			t.Fatal(err)
		}
		return catalog
	}
	oldCatalog := openPaused(func(event sessioncatalog.DiscoveryEvent) {
		if event.Phase == "requested" {
			oldRequests.Add(1)
		}
	})
	app.sessionCatalog.Store(oldCatalog)
	entered := make(chan struct{}, 2)
	oldRelease, newRelease := make(chan struct{}), make(chan struct{})
	var oldOnce, newOnce sync.Once
	var calls atomic.Int32
	app.catalogReconcileHook = func(sessioncatalog.DirectoryTarget) {
		call := calls.Add(1)
		entered <- struct{}{}
		if call == 1 {
			<-oldRelease
		} else {
			<-newRelease
		}
	}
	var jobs []<-chan struct{}
	t.Cleanup(func() {
		oldOnce.Do(func() { close(oldRelease) })
		newOnce.Do(func() { close(newRelease) })
		cancel()
		if !app.stopSessionCatalog(sessionCatalogTestDeadline) {
			t.Error("replacement catalog fixture did not close cleanly")
		}
		for _, done := range jobs {
			if !waitChannelBefore(done, time.Now().Add(sessionCatalogTestDeadline)) {
				t.Error("retired reconcile outlived its fixture")
			}
		}
	})
	dir := t.TempDir()
	if !app.requestSessionCatalogReconcile(dir) {
		t.Fatal("old reconcile was not admitted")
	}
	key := projectRootKey(dir)
	app.catalogReconcileMu.Lock()
	oldJob := app.catalogReconcileJobs[key]
	app.catalogReconcileMu.Unlock()
	jobs = append(jobs, oldJob.done)
	<-entered
	if app.stopSessionCatalog(25 * time.Millisecond) {
		t.Fatal("stop joined the old job before its hook released")
	}

	// A timed-out rebuild may publish the next catalog while the old hook is
	// still unwinding. The same directory must receive a distinct job owner.
	app.sessionCatalog.Store(openPaused(nil))
	if !app.requestSessionCatalogReconcile(dir) {
		t.Fatal("replacement reconcile was not admitted")
	}
	app.catalogReconcileMu.Lock()
	newJob := app.catalogReconcileJobs[key]
	app.catalogReconcileMu.Unlock()
	jobs = append(jobs, newJob.done)
	if oldJob == newJob {
		t.Fatal("replacement catalog inherited a retired reconcile job")
	}
	<-entered
	oldOnce.Do(func() { close(oldRelease) })
	if !waitChannelBefore(oldJob.done, time.Now().Add(time.Second)) {
		t.Fatal("retired reconcile did not exit after its hook released")
	}
	app.catalogReconcileMu.Lock()
	currentJob := app.catalogReconcileJobs[key]
	app.catalogReconcileMu.Unlock()
	if currentJob != newJob {
		t.Fatal("retired reconcile removed the replacement job")
	}
	if oldRequests.Load() != 0 {
		t.Fatal("retired reconcile scheduled discovery after cancellation")
	}
	newOnce.Do(func() { close(newRelease) })
	if !app.stopSessionCatalog(time.Second) {
		t.Fatal("replacement reconcile did not stop cleanly")
	}
}

func TestInvalidatedSessionCatalogReleasesQueuedReconcileWithoutAppShutdown(t *testing.T) {
	isolateDesktopUserDirs(t)
	ctx, cancel := context.WithCancel(t.Context())
	app := NewApp()
	app.ctx = ctx
	requested := make(chan struct{}, 1)
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	catalog, err := sessioncatalog.Open(ctx, sessioncatalog.Options{
		Path: path, MetadataOnly: true, StartPaused: true,
		OnDiscovery: func(event sessioncatalog.DiscoveryEvent) {
			if event.Phase == "requested" {
				requested <- struct{}{}
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	app.sessionCatalog.Store(catalog)
	var jobDone <-chan struct{}
	t.Cleanup(func() {
		cancel()
		if !app.stopSessionCatalog(sessionCatalogTestDeadline) {
			t.Error("invalidated catalog fixture did not close cleanly")
		}
		if jobDone != nil && !waitChannelBefore(jobDone, time.Now().Add(sessionCatalogTestDeadline)) {
			t.Error("invalidated reconcile outlived its fixture")
		}
	})
	dir := t.TempDir()
	if !app.requestSessionCatalogReconcile(dir) {
		t.Fatal("explicit reconcile was not admitted")
	}
	app.catalogReconcileMu.Lock()
	jobDone = app.catalogReconcileJobs[projectRootKey(dir)].done
	app.catalogReconcileMu.Unlock()
	<-requested

	// Corrupt this disposable generation's root page. A fresh public read lease
	// must report corruption and revoke it while discovery is still paused.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`PRAGMA writable_schema=ON; UPDATE sqlite_master SET rootpage=2147483647 WHERE type='table' AND name='catalog_state'; PRAGMA schema_version=999`)
	closeErr := db.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	lease, err := catalog.OpenReadLease(ctx)
	if lease != nil {
		lease.Close()
	}
	if err == nil {
		t.Fatal("corrupt root page remained readable")
	}
	select {
	case <-catalog.Invalidated():
	default:
		t.Fatalf("public read did not invalidate the corrupt generation: %v", err)
	}
	if !waitChannelBefore(jobDone, time.Now().Add(time.Second)) {
		t.Fatal("invalidated discovery kept the desktop job waiting for app shutdown")
	}
	if ctx.Err() != nil {
		t.Fatal("reconcile completion depended on app shutdown")
	}
}
