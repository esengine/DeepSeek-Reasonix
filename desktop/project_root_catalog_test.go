package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"reasonix/internal/sessioncatalog"
)

func TestSessionCatalogTargetsIncludeRestoredProjectTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	root := t.TempDir()
	sessionDir := desktopSessionDir(root)
	app.tabs["restored"] = &WorkspaceTab{
		ID: "restored", Scope: "project", WorkspaceRoot: root,
		SessionPath: filepath.Join(sessionDir, "restored.jsonl"),
	}
	for _, target := range app.sessionCatalogTargets() {
		if target.Path == sessionDir && target.Scope == "project" && target.WorkspaceRoot == root {
			return
		}
	}
	t.Fatalf("session catalog targets = %#v, want restored project directory %q", app.sessionCatalogTargets(), sessionDir)
}

func waitForCatalogSessionPath(t *testing.T, app *App, workspaceRoot, sessionDir, sessionPath string) sessioncatalog.SessionRecord {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last []sessioncatalog.SessionRecord
	for time.Now().Before(deadline) {
		catalog := app.sessionCatalog.Load()
		if catalog == nil {
			t.Fatal("session catalog is not installed")
		}
		page, err := catalog.ListSessions(context.Background(), sessioncatalog.SessionPageRequest{
			Scope: "project", WorkspaceRoot: workspaceRoot, Directory: sessionDir, Limit: 20,
		})
		if err != nil {
			t.Fatalf("list project sessions: %v", err)
		}
		last = page.Items
		for _, item := range page.Items {
			if item.Path == sessionPath {
				return item
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("project session %q did not become visible: %#v", sessionPath, last)
	return sessioncatalog.SessionRecord{}
}

func TestRegisterProjectRootReconcilesNewProjectSessionDirectory(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	globalDir := desktopSessionDir(globalWorkspaceRoot())
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("mkdir global session dir: %v", err)
	}
	installSessionCatalogForTest(t, app, globalDir, "global", "")

	root := t.TempDir()
	sessionDir := desktopSessionDir(root)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir project session dir: %v", err)
	}
	sessionPath := writeTopicSession(t, sessionDir, "existing.jsonl", "topic-existing", "Existing topic", root)

	app.registerProjectRoot(root)

	session := waitForCatalogSessionPath(t, app, root, sessionDir, sessionPath)
	if session.TopicTitle != "Existing topic" {
		t.Fatalf("session topic title = %q, want Existing topic", session.TopicTitle)
	}
}

func TestRegisterProjectRootDoesNotRescanExistingProjectOnActivation(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	globalDir := desktopSessionDir(globalWorkspaceRoot())
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	installSessionCatalogForTest(t, app, globalDir, "global", "")
	root := t.TempDir()
	if err := os.MkdirAll(desktopSessionDir(root), 0o755); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	done := make(chan struct{}, 1)
	app.catalogReconcileHook = func(sessioncatalog.DirectoryTarget) {
		started <- struct{}{}
		<-release
	}
	app.catalogReconcileDoneHook = func(sessioncatalog.DirectoryTarget) { done <- struct{}{} }
	app.registerProjectRoot(root)
	<-started
	for range 25 {
		app.registerProjectRoot(root)
	}
	close(release)
	<-done
	select {
	case <-started:
		t.Fatal("activating an already registered project scheduled another full reconcile")
	default:
	}
}

func TestExplicitReconcileCoalescesConcurrentRequests(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	dir := t.TempDir()
	installSessionCatalogForTest(t, app, dir, "global", "")

	started := make(chan struct{}, 2)
	releaseFirst := make(chan struct{})
	done := make(chan struct{}, 1)
	var calls int
	var callsMu sync.Mutex
	app.catalogReconcileHook = func(sessioncatalog.DirectoryTarget) {
		callsMu.Lock()
		calls++
		call := calls
		callsMu.Unlock()
		started <- struct{}{}
		if call == 1 {
			<-releaseFirst
		}
	}
	app.catalogReconcileDoneHook = func(sessioncatalog.DirectoryTarget) { done <- struct{}{} }
	app.requestSessionCatalogReconcile(dir)
	<-started
	for range 25 {
		app.requestSessionCatalogReconcile(dir)
	}
	close(releaseFirst)
	<-started
	<-done
	callsMu.Lock()
	defer callsMu.Unlock()
	if calls != 2 {
		t.Fatalf("reconcile passes = %d, want one active pass plus one coalesced dirty pass", calls)
	}
}

func TestStopSessionCatalogWaitsForExplicitReconcile(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	dir := t.TempDir()
	installSessionCatalogForTest(t, app, dir, "global", "")

	started := make(chan struct{})
	release := make(chan struct{})
	app.catalogReconcileHook = func(sessioncatalog.DirectoryTarget) {
		close(started)
		<-release
	}
	app.requestSessionCatalogReconcile(dir)
	<-started
	stopped := make(chan struct{})
	go func() {
		app.stopSessionCatalog(time.Second)
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("catalog stopped before its explicit reconcile released")
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("catalog stop did not drain its explicit reconcile")
	}
}
