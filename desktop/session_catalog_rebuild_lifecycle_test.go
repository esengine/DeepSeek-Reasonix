package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/sessioncatalog"
)

func waitForSessionCatalogForTest(t *testing.T, app *App, previous *sessioncatalog.Catalog) *sessioncatalog.Catalog {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		catalog := app.sessionCatalog.Load()
		if catalog != nil && catalog != previous {
			return catalog
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("session catalog was not published before the deadline")
	return nil
}

func assertSessionCatalogWatcherRunning(t *testing.T, app *App) {
	t.Helper()
	app.catalogLifecycleMu.Lock()
	cancel := app.catalogCancel
	done := app.catalogDone
	app.catalogLifecycleMu.Unlock()
	if cancel == nil || done == nil {
		t.Fatal("session catalog watcher is not armed")
	}
	select {
	case <-done:
		t.Fatal("session catalog watcher exited instead of entering the refresh loop")
	default:
	}
}

func TestSessionCatalogRebuildingStatusPreservesPublishedCounts(t *testing.T) {
	isolateDesktopUserDirs(t)
	catalog, err := sessioncatalog.Open(context.Background(), sessioncatalog.Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "counted.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := catalog.UpsertSession(context.Background(), sessioncatalog.SessionRecord{
		Path: path, Directory: dir, Scope: "global", TopicID: "counted",
		Turns: 1, TurnsState: sessioncatalog.TurnsValid, Health: sessioncatalog.HealthOK,
	}); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.sessionCatalog.Store(catalog)
	published := sessionCatalogStatus(catalog.Status())
	app.catalogRebuilding.Store(true)
	t.Cleanup(func() {
		app.catalogRebuilding.Store(false)
		app.sessionCatalog.CompareAndSwap(catalog, nil)
		_ = catalog.Close(context.Background())
	})

	status := app.currentSessionCatalogStatus()
	if status.State != string(sessioncatalog.StateRebuilding) {
		t.Fatalf("state = %q, want rebuilding", status.State)
	}
	if status.Indexed != published.Indexed || status.Total != published.Total || status.Revision != published.Revision {
		t.Fatalf("rebuilding snapshot = %d/%d@%d, want published %d/%d@%d",
			status.Indexed, status.Total, status.Revision, published.Indexed, published.Total, published.Revision)
	}
	if status.CanRebuild {
		t.Fatal("an active rebuild must fail closed for another rebuild request")
	}

	app.sessionCatalog.Store(nil)
	status = app.currentSessionCatalogStatus()
	if status.State != string(sessioncatalog.StateRebuilding) || status.Total != 0 || status.CanRebuild {
		t.Fatalf("catalog-close rebuild status = %+v, want unknown progress and no rebuild action", status)
	}
}

func TestSessionCatalogStatusCanRebuildJSONContract(t *testing.T) {
	ready := sessionCatalogStatus(sessioncatalog.Status{State: sessioncatalog.StateReady, Mode: sessioncatalog.ModeDisk})
	degraded := sessionCatalogStatus(sessioncatalog.Status{State: sessioncatalog.StateDegraded, Mode: sessioncatalog.ModeMemory})
	failed := sessionCatalogStatus(sessioncatalog.Status{State: sessioncatalog.StateReady, LastError: "catalog unavailable"})
	opening := sessionCatalogStatus(sessioncatalog.Status{State: sessioncatalog.StateOpening, LastError: "still opening"})
	closed := sessionCatalogStatus(sessioncatalog.Status{State: sessioncatalog.StateClosed, LastError: "closed"})
	repairing := sessionCatalogStatus(sessioncatalog.Status{State: sessioncatalog.StateDegraded, RepairPending: 1})
	if ready.CanRebuild || !degraded.CanRebuild || !failed.CanRebuild || opening.CanRebuild || closed.CanRebuild || repairing.CanRebuild {
		t.Fatalf("canRebuild policy ready/degraded/failed/opening/closed/repairing = %v/%v/%v/%v/%v/%v",
			ready.CanRebuild, degraded.CanRebuild, failed.CanRebuild, opening.CanRebuild, closed.CanRebuild, repairing.CanRebuild)
	}
	for _, status := range []SessionCatalogStatus{ready, degraded, failed, opening, closed, repairing} {
		body, err := json.Marshal(status)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), `"canRebuild":`) {
			t.Fatalf("Wails status omitted canRebuild: %s", body)
		}
	}
}

func TestRebuildSessionCatalogReturnsWithOrdinaryWatcherRunning(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	app.startSessionCatalog()
	oldCatalog := waitForSessionCatalogForTest(t, app, nil)
	t.Cleanup(func() { app.stopSessionCatalog(time.Second) })
	for range 32 {
		if err := oldCatalog.SyncMetadata(context.Background(), nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	oldRevision := oldCatalog.Status().Revision
	app.ctx = context.Background()
	events := make(chan ProjectTreeChangedV2, 8)
	app.runtimeEvents.emit = func(_ context.Context, name string, payload ...any) {
		if name != "project-tree:changed-v2" || len(payload) != 1 {
			return
		}
		if event, ok := payload[0].(ProjectTreeChangedV2); ok {
			events <- event
		}
	}

	if err := app.RebuildSessionCatalog(); err != nil {
		t.Fatal(err)
	}
	if app.catalogRebuilding.Load() {
		t.Fatal("RebuildSessionCatalog returned while rebuilding was still true")
	}
	assertSessionCatalogWatcherRunning(t, app)
	newCatalog := waitForSessionCatalogForTest(t, app, oldCatalog)
	if status := newCatalog.Status(); status.State != sessioncatalog.StateReady {
		t.Fatalf("replacement watcher status = %q, want ready", status.State)
	}
	if revision := newCatalog.Status().Revision; revision < oldRevision {
		t.Fatalf("replacement watcher revision = %d, want at least previous revision %d", revision, oldRevision)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Reason != "catalog_rebuild_finished" {
				continue
			}
			if event.Revision < oldRevision {
				t.Fatalf("finished event revision = %d, want at least previous revision %d", event.Revision, oldRevision)
			}
			goto finishedEventObserved
		case <-deadline:
			t.Fatal("catalog rebuild finished event was not published")
		}
	}

finishedEventObserved:
	if app.catalogRebuilding.Load() {
		t.Fatal("the long-lived watcher took ownership of rebuilding")
	}
}

func TestRebuildSessionCatalogFailureKeepsProjectionAndRestartsWatcher(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "keep.jsonl")
	body := []byte(`{"role":"user","content":"keep me"}` + "\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMetaPreserveUpdated(path, agent.BranchMeta{
		ID: agent.BranchID(path), Scope: "global", TopicID: "keep", TopicTitle: "Keep",
	}); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.startSessionCatalog()
	oldCatalog := waitForSessionCatalogForTest(t, app, nil)
	t.Cleanup(func() { app.stopSessionCatalog(time.Second) })
	if err := oldCatalog.ReconcileDirectory(context.Background(), sessioncatalog.DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := oldCatalog.GetTopic(context.Background(), sessioncatalog.TopicKey{Scope: "global", TopicID: "keep"}); err != nil || !ok {
		t.Fatalf("seed topic missing before failed rebuild: ok=%v err=%v", ok, err)
	}
	metaPath := agent.BranchMetaPath(path)
	var metaBody []byte
	var err error
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		metaBody, err = os.ReadFile(metaPath)
		if err == nil && strings.Contains(string(metaBody), `"schema_version": 2`) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(string(metaBody), `"schema_version": 2`) {
		t.Fatalf("listing sidecar did not settle before rebuild: %v: %s", err, metaBody)
	}

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

	if err := app.RebuildSessionCatalog(); err == nil {
		t.Fatal("expected rebuild to fail for a session target that is a regular file")
	}
	if app.catalogRebuilding.Load() {
		t.Fatal("failed rebuild left rebuilding set")
	}
	assertSessionCatalogWatcherRunning(t, app)
	newCatalog := waitForSessionCatalogForTest(t, app, oldCatalog)
	if _, ok, err := newCatalog.GetTopic(context.Background(), sessioncatalog.TopicKey{Scope: "global", TopicID: "keep"}); err != nil || !ok {
		t.Fatalf("failed rebuild lost the existing projection: ok=%v err=%v", ok, err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(body) {
		t.Fatalf("failed rebuild modified the authoritative transcript: got %q want %q", after, body)
	}
	metaAfter, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(metaAfter) != string(metaBody) {
		t.Fatalf("failed rebuild modified the authoritative sidecar: got %q want %q", metaAfter, metaBody)
	}
}

func TestRebuildSessionCatalogDoesNotRestartDuringShutdown(t *testing.T) {
	app := NewApp()
	app.shuttingDown.Store(true)
	if err := app.RebuildSessionCatalog(); err == nil {
		t.Fatal("rebuild during shutdown must fail")
	}
	app.catalogLifecycleMu.Lock()
	cancel, done := app.catalogCancel, app.catalogDone
	app.catalogLifecycleMu.Unlock()
	if cancel != nil || done != nil || app.catalogRebuilding.Load() {
		t.Fatal("shutdown rebuild armed catalog lifecycle state")
	}
}
