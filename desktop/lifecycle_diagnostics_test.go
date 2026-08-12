package main

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func lifecycleTrackerForTest(t *testing.T, root string, pid int, runID string) *desktopLifecycleTracker {
	t.Helper()
	tracker := newDesktopLifecycleTracker(root, "v1.23.0", "stable")
	tracker.state.PID = pid
	tracker.state.RunID = runID
	tracker.path = filepath.Join(tracker.dir, runID+".json")
	tracker.processAlive = func(int) bool { return false }
	return tracker
}

func TestDesktopLifecycleDeadRecordIsConsumedOnce(t *testing.T) {
	root := t.TempDir()
	dead := lifecycleTrackerForTest(t, root, 4242, "dead")
	if err := dead.start(); err != nil {
		t.Fatal(err)
	}
	dead.mark("healthy")
	dead.stopWriter()

	reader := lifecycleTrackerForTest(t, root, os.Getpid(), "reader")
	got := reader.consumePrevious(true)
	if len(got) != 1 || got[0].Phase != "healthy" || got[0].Version != "v1.23.0" {
		t.Fatalf("observations = %+v", got)
	}
	if replay := reader.consumePrevious(true); len(replay) != 0 {
		t.Fatalf("lifecycle record replayed: %+v", replay)
	}
}

func TestDesktopLifecycleConcurrentConsumersClaimOnce(t *testing.T) {
	root := t.TempDir()
	dead := lifecycleTrackerForTest(t, root, 4242, "dead-concurrent")
	if err := dead.start(); err != nil {
		t.Fatal(err)
	}
	dead.stopWriter()

	const observers = 16
	start := make(chan struct{})
	var ready sync.WaitGroup
	var group sync.WaitGroup
	var reports atomic.Int32
	for observer := range observers {
		ready.Add(1)
		group.Go(func() {
			reader := lifecycleTrackerForTest(t, root, os.Getpid(), "reader-"+strconv.Itoa(observer))
			ready.Done()
			<-start
			if len(reader.consumePrevious(true)) == 1 {
				reports.Add(1)
			}
		})
	}
	ready.Wait()
	close(start)
	group.Wait()
	if got := reports.Load(); got != 1 {
		t.Fatalf("concurrent reports = %d, want 1", got)
	}
}

func TestDesktopDiagnosticsOwnershipIsNonBlockingAndExclusive(t *testing.T) {
	oldVersion := version
	version = "v1.23.0"
	t.Cleanup(func() { version = oldVersion })

	first := NewApp()
	prepareDesktopDiagnostics(first)
	t.Cleanup(func() {
		first.lifecycle.tracker.clean()
		first.releaseDesktopDiagnosticsOwnership()
	})
	if !first.diagnosticsOwner {
		t.Fatal("first process did not claim diagnostics ownership")
	}

	second := NewApp()
	prepareDesktopDiagnostics(second)
	if second.diagnosticsOwner {
		second.releaseDesktopDiagnosticsOwnership()
		t.Fatal("second process unexpectedly claimed diagnostics ownership")
	}

	first.lifecycle.tracker.clean()
	first.releaseDesktopDiagnosticsOwnership()
	prepareDesktopDiagnostics(second)
	t.Cleanup(func() {
		second.lifecycle.tracker.clean()
		second.releaseDesktopDiagnosticsOwnership()
	})
	if !second.diagnosticsOwner {
		t.Fatal("ownership was not released for the next process")
	}
}

func TestDesktopDiagnosticsSkipsNonPrimaryLaunchModes(t *testing.T) {
	oldVersion := version
	t.Cleanup(func() {
		version = oldVersion
	})

	remote := NewApp()
	remote.remoteWindowTicket = "remote"
	prepareDesktopDiagnostics(remote)
	if remote.diagnosticsOwner {
		t.Fatal("remote window claimed diagnostics ownership")
	}

	version = "dev"
	dev := NewApp()
	prepareDesktopDiagnostics(dev)
	if dev.diagnosticsOwner {
		t.Fatal("dev build claimed diagnostics ownership")
	}

}

func TestDesktopLifecycleLiveRecordIsPreserved(t *testing.T) {
	root := t.TempDir()
	live := lifecycleTrackerForTest(t, root, 4242, "live")
	if err := live.start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(live.stopWriter)

	reader := lifecycleTrackerForTest(t, root, os.Getpid(), "reader")
	reader.processAlive = func(pid int) bool { return pid == 4242 }
	if got := reader.consumePrevious(true); len(got) != 0 {
		t.Fatalf("live record observed: %+v", got)
	}
	if _, err := os.Stat(live.path); err != nil {
		t.Fatalf("live record was removed: %v", err)
	}
}

func TestDesktopLifecycleOptOutConsumesWithoutReporting(t *testing.T) {
	root := t.TempDir()
	dead := lifecycleTrackerForTest(t, root, 4242, "dead")
	if err := dead.start(); err != nil {
		t.Fatal(err)
	}
	dead.stopWriter()

	reader := lifecycleTrackerForTest(t, root, os.Getpid(), "reader")
	if got := reader.consumePrevious(false); len(got) != 0 {
		t.Fatalf("opt-out returned observations: %+v", got)
	}
	if _, err := os.Stat(dead.path); !os.IsNotExist(err) {
		t.Fatalf("opt-out did not consume dead record: %v", err)
	}
}

func TestDesktopLifecycleUnknownSchemaIsPreserved(t *testing.T) {
	root := t.TempDir()
	future := lifecycleTrackerForTest(t, root, 4242, "future")
	future.state.SchemaVersion = desktopLifecycleSchemaVersion + 1
	if err := future.start(); err != nil {
		t.Fatal(err)
	}
	future.stopWriter()

	reader := lifecycleTrackerForTest(t, root, os.Getpid(), "reader")
	if got := reader.consumePrevious(true); len(got) != 0 {
		t.Fatalf("future lifecycle record observed: %+v", got)
	}
	state, err := readDesktopLifecycleState(future.path)
	if err != nil || state.SchemaVersion != desktopLifecycleSchemaVersion+1 {
		t.Fatalf("future lifecycle record was not preserved: state=%+v err=%v", state, err)
	}
}

func TestDesktopLifecycleUnknownSchemaWithoutCurrentFieldsIsNeverPruned(t *testing.T) {
	root := t.TempDir()
	future := lifecycleTrackerForTest(t, root, 4242, "future-fields")
	future.state.SchemaVersion = desktopLifecycleSchemaVersion + 1
	future.state.PID = 0
	future.state.Phase = ""
	if err := future.start(); err != nil {
		t.Fatal(err)
	}
	future.stopWriter()
	old := time.Now().UTC().Add(-2 * desktopLifecycleRetention)
	if err := os.Chtimes(future.path, old, old); err != nil {
		t.Fatal(err)
	}

	reader := lifecycleTrackerForTest(t, root, os.Getpid(), "reader")
	reader.now = func() time.Time { return time.Now().UTC() }
	if got := reader.consumePrevious(true); len(got) != 0 {
		t.Fatalf("future lifecycle record observed: %+v", got)
	}
	if _, err := os.Stat(future.path); err != nil {
		t.Fatalf("future lifecycle record without v2 fields was pruned: %v", err)
	}
}

func TestDesktopLifecycleCleanRemovesCurrentRecord(t *testing.T) {
	tracker := lifecycleTrackerForTest(t, t.TempDir(), os.Getpid(), "current")
	base := time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)
	tracker.now = func() time.Time { return base }
	if err := tracker.start(); err != nil {
		t.Fatal(err)
	}
	tracker.mark("shutting_down")
	state, err := readDesktopLifecycleState(tracker.path)
	if err != nil || state.Phase != "shutting_down" || state.UpdatedAt != base.Format(time.RFC3339Nano) {
		t.Fatalf("state = %+v err=%v", state, err)
	}
	tracker.clean()
	if _, err := os.Stat(tracker.path); !os.IsNotExist(err) {
		t.Fatalf("clean lifecycle record remains: %v", err)
	}
}

func TestDesktopLifecycleAsyncWriterKeepsLatestPhase(t *testing.T) {
	tracker := lifecycleTrackerForTest(t, t.TempDir(), os.Getpid(), "async")
	if err := tracker.start(); err != nil {
		t.Fatal(err)
	}
	tracker.markAsync("ready")
	tracker.markAsync("healthy")
	tracker.stopWriter()

	state, err := readDesktopLifecycleState(tracker.path)
	if err != nil || state.Phase != "healthy" {
		t.Fatalf("async lifecycle state = %+v err=%v", state, err)
	}
}

func TestDesktopShutdownPanicPreservesLifecycleRecord(t *testing.T) {
	tracker := lifecycleTrackerForTest(t, t.TempDir(), os.Getpid(), "shutdown-panic")
	if err := tracker.start(); err != nil {
		t.Fatal(err)
	}

	func() {
		defer func() { _ = recover() }()
		completeDesktopShutdown(tracker, func() { panic("teardown failed") })
	}()
	state, err := readDesktopLifecycleState(tracker.path)
	if err != nil || state.Phase != "shutting_down" {
		t.Fatalf("panic lifecycle state = %+v err=%v", state, err)
	}
}
