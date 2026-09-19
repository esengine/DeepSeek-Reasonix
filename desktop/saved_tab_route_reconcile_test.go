package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/desktop/internal/workspacestate"
)

func TestReconcileSavedTabsNormalizesExactCanonicalRoute(t *testing.T) {
	app := newSavedTabReconcileTestApp(t)
	root := t.TempDir()
	ref, workspaceID := createLegacyCleanupSession(t, app, root, "exact-route", true)
	finishSavedTabMigration(app)
	entry := savedProjectTab("saved", "", "", root, workspaceID)
	entry.SessionPath = sessionRoute(ref.SessionID)
	entry.extra = map[string]json.RawMessage{"future": json.RawMessage(`{"kept":true}`)}

	got, changed := app.reconcileSavedTabs(t.Context(), desktopTabsFile{Tabs: []desktopTabEntry{entry}, ActiveTab: entry.ID})
	if !changed || len(got.Tabs) != 1 {
		t.Fatalf("exact route reconciliation = changed:%v file:%+v", changed, got)
	}
	if got.Tabs[0].SessionID != ref.SessionID || got.Tabs[0].SessionPath != "" {
		t.Fatalf("exact route identity = id:%q path:%q", got.Tabs[0].SessionID, got.Tabs[0].SessionPath)
	}
	if string(got.Tabs[0].extra["future"]) != `{"kept":true}` {
		t.Fatalf("unknown entry field lost: %s", mustMarshalJSON(t, got.Tabs[0]))
	}
	second, changedAgain := app.reconcileSavedTabs(t.Context(), got)
	if changedAgain || second.Tabs[0].SessionID != ref.SessionID || second.Tabs[0].SessionPath != "" {
		t.Fatalf("route repair was not idempotent: changed=%v file=%+v", changedAgain, second)
	}
}

func TestReconcileSavedTabsNormalizesWindowsPseudoRouteWithCanonicalEvidence(t *testing.T) {
	app := newSavedTabReconcileTestApp(t)
	root := t.TempDir()
	ref, workspaceID := createLegacyCleanupSession(t, app, root, "windows-pseudo-route", true)
	finishSavedTabMigration(app)
	entry := savedProjectTab("saved", "", "", root, workspaceID)
	entry.SessionPath = `C:\old\reasonix\session-id:` + ref.SessionID

	got, changed := app.reconcileSavedTabs(t.Context(), desktopTabsFile{Tabs: []desktopTabEntry{entry}})
	if !changed || len(got.Tabs) != 1 || got.Tabs[0].SessionID != ref.SessionID || got.Tabs[0].SessionPath != "" {
		t.Fatalf("pseudo route reconciliation = changed:%v file:%+v", changed, got)
	}
}

func TestReconcileSavedTabsBlocksConflictingAndInvalidRoutes(t *testing.T) {
	for _, test := range []struct {
		name  string
		entry desktopTabEntry
	}{
		{name: "conflicting ids", entry: desktopTabEntry{ID: "conflict", Scope: "global", SessionID: "other", SessionPath: "session-id:route-id"}},
		{name: "invalid route", entry: desktopTabEntry{ID: "invalid", Scope: "global", SessionPath: "session-id:bad/id"}},
		{name: "sidecar is not transcript", entry: desktopTabEntry{ID: "sidecar", Scope: "global", SessionPath: filepath.Join(t.TempDir(), "session-id:route.meta")}},
		{name: "relative legacy path", entry: desktopTabEntry{ID: "relative", Scope: "global", SessionPath: "relative.jsonl"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := newSavedTabReconcileTestApp(t)
			finishSavedTabMigration(app)
			got, changed := app.reconcileSavedTabs(t.Context(), desktopTabsFile{Tabs: []desktopTabEntry{test.entry}})
			if changed || len(got.Tabs) != 1 || !got.Tabs[0].restoreBlocked {
				t.Fatalf("blocked route = changed:%v file:%+v", changed, got)
			}
			if got.Tabs[0].SessionID != test.entry.SessionID || got.Tabs[0].SessionPath != test.entry.SessionPath {
				t.Fatalf("conflicting identity was modified: got=%+v want=%+v", got.Tabs[0], test.entry)
			}
		})
	}
}

func TestReconcileSavedTabsPreservesRealPseudoRouteArtifact(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permits colons in file names")
	}
	app := newSavedTabReconcileTestApp(t)
	root := t.TempDir()
	ref, workspaceID := createLegacyCleanupSession(t, app, root, "real-artifact-route", true)
	finishSavedTabMigration(app)
	path := filepath.Join(t.TempDir(), sessionRoute(ref.SessionID))
	if err := os.WriteFile(path, []byte("legacy content"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := savedProjectTab("saved", "", "", root, workspaceID)
	entry.SessionPath = path

	got, changed := app.reconcileSavedTabs(t.Context(), desktopTabsFile{Tabs: []desktopTabEntry{entry}})
	if changed || len(got.Tabs) != 1 || !got.Tabs[0].restoreBlocked || got.Tabs[0].SessionPath != path {
		t.Fatalf("real pseudo-route artifact was rewritten: changed=%v file=%+v", changed, got)
	}
}

func TestPseudoRouteArtifactProbePreservesUnreadableResult(t *testing.T) {
	want := errors.New("permission denied")
	present, err := savedTabPseudoRouteArtifactsPresentWith(filepath.Join(t.TempDir(), "session-id:probe"), func(string) (os.FileInfo, error) {
		return nil, want
	})
	if present || !errors.Is(err, want) {
		t.Fatalf("artifact probe = present:%v err:%v", present, err)
	}
}

func TestRestoreBlocksInvalidRouteWithoutControllerOrSidecar(t *testing.T) {
	app := newSavedTabReconcileTestApp(t)
	finishSavedTabMigration(app)
	entry := desktopTabEntry{ID: "invalid", Scope: "global", TopicID: "topic", SessionPath: "session-id:bad/id"}
	file := desktopTabsFile{Tabs: []desktopTabEntry{entry}, ActiveTab: entry.ID, TabOrder: []string{entry.ID}}
	if err := os.MkdirAll(desktopConfigDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(desktopConfigDir(), tabsFileName), mustMarshalJSON(t, file), 0o600); err != nil {
		t.Fatal(err)
	}
	app.tabsRestored = make(chan struct{})

	app.restoreOrBuildTabs()
	tab := app.tabs[entry.ID]
	if tab == nil || tab.Ctrl != nil || tab.StartupErr == "" {
		t.Fatalf("blocked restore tab = %+v", tab)
	}
	if len(app.runtimeByID) != 0 || len(app.runtimeBySessionKey) != 0 {
		t.Fatalf("blocked restore created runtime state: %d/%d", len(app.runtimeByID), len(app.runtimeBySessionKey))
	}
	for _, artifact := range []string{"session-id:bad/id.meta", "session-id:bad/id.lock", "session-id:bad/id.lease.lock"} {
		_, err := os.Lstat(artifact)
		if runtime.GOOS == "windows" && err != nil {
			// The route colon makes the spelling unrepresentable on Windows.
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("blocked route created %q: %v", artifact, err)
		}
	}
}

func TestReconcileTabsBeforeRestoreBlocksOriginalWhenRepairWriteFails(t *testing.T) {
	app := newSavedTabReconcileTestApp(t)
	root := t.TempDir()
	ref, workspaceID := createLegacyCleanupSession(t, app, root, "write-failure-route", true)
	finishSavedTabMigration(app)
	entry := savedProjectTab("saved", "", "", root, workspaceID)
	entry.SessionPath = sessionRoute(ref.SessionID)
	file := desktopTabsFile{Tabs: []desktopTabEntry{entry}, ActiveTab: entry.ID}

	destination := filepath.Join(desktopConfigDir(), tabsFileName)
	if err := os.RemoveAll(destination); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	got, _, current := app.reconcileTabsBeforeRestore(t.Context(), file, app.tabsSnapshotVersion())
	if !current || len(got.Tabs) != 1 || !got.Tabs[0].restoreBlocked {
		t.Fatalf("failed repair write restore = current:%v file:%+v", current, got)
	}
	if got.Tabs[0].SessionID != "" || got.Tabs[0].SessionPath != entry.SessionPath {
		t.Fatalf("failed repair write published unpersisted identity: %+v", got.Tabs[0])
	}
}

func TestReconcileTabsBeforeRestoreRejectsRouteRepairAfterConcurrentSave(t *testing.T) {
	app := newSavedTabReconcileTestApp(t)
	root := t.TempDir()
	ref, workspaceID := createLegacyCleanupSession(t, app, root, "concurrent-route", true)
	entry := savedProjectTab("saved", "", "", root, workspaceID)
	entry.SessionPath = sessionRoute(ref.SessionID)
	file := desktopTabsFile{Tabs: []desktopTabEntry{entry}, ActiveTab: entry.ID}
	reached := make(chan struct{})
	app.beforeSavedTabMigrationWait = func() { close(reached) }
	type result struct {
		file    desktopTabsFile
		current bool
	}
	done := make(chan result, 1)
	version := app.tabsSnapshotVersion()
	go func() {
		got, _, current := app.reconcileTabsBeforeRestore(t.Context(), file, version)
		done <- result{file: got, current: current}
	}()
	<-reached
	app.mu.Lock()
	app.tabsSaveVersion++
	app.mu.Unlock()
	finishSavedTabMigration(app)
	got := <-done
	if got.current {
		t.Fatal("stale startup reconciliation remained publishable")
	}
	if got.file.Tabs[0].SessionID != "" || got.file.Tabs[0].SessionPath != entry.SessionPath {
		t.Fatalf("stale repair escaped its snapshot: %+v", got.file.Tabs[0])
	}
}

func TestReconcileSavedTabsPseudoRouteUsesPendingAndRecoveryEvidence(t *testing.T) {
	t.Run("pending create", func(t *testing.T) {
		app := newSavedTabReconcileTestApp(t)
		root := t.TempDir()
		workspaceID, err := app.ensureDesktopWorkspace(t.Context(), "project", root)
		if err != nil {
			t.Fatal(err)
		}
		const sessionID = "pending-pseudo-route"
		const operationID = "pending-pseudo-operation"
		if err := app.workspaceRegistry().BeginCreate(t.Context(), workspacestate.PendingCreate{
			OperationID: operationID, WorkspaceID: workspaceID, SessionID: sessionID,
		}); err != nil {
			t.Fatal(err)
		}
		finishSavedTabMigration(app)
		entry := savedProjectTab("pending", "", operationID, root, workspaceID)
		entry.SessionPath = `C:\old\session-id:` + sessionID
		got, changed := app.reconcileSavedTabs(t.Context(), desktopTabsFile{Tabs: []desktopTabEntry{entry}})
		if !changed || len(got.Tabs) != 1 || got.Tabs[0].SessionID != sessionID || got.Tabs[0].SessionPath != "" || got.Tabs[0].restoreBlocked {
			t.Fatalf("pending pseudo-route evidence = changed:%v file:%+v", changed, got)
		}
	})

	t.Run("recovery owner", func(t *testing.T) {
		app := newSavedTabReconcileTestApp(t)
		root := t.TempDir()
		workspaceID, err := app.ensureDesktopWorkspace(t.Context(), "project", root)
		if err != nil {
			t.Fatal(err)
		}
		const sessionID = "recovery-pseudo-route"
		if err := app.workspaceRegistry().RecordRecovery(t.Context(), workspacestate.RecoveryEntry{
			ID: "recovery-pseudo", SourceKey: "pseudo", SessionID: sessionID, WorkspaceID: workspaceID,
			Scope: "project", WorkspaceRoot: root, Format: "canonical", Reason: "interrupted", Status: "pending",
		}); err != nil {
			t.Fatal(err)
		}
		finishSavedTabMigration(app)
		entry := savedProjectTab("recovery", "", "", root, workspaceID)
		entry.SessionPath = `C:\old\session-id:` + sessionID
		got, changed := app.reconcileSavedTabs(t.Context(), desktopTabsFile{Tabs: []desktopTabEntry{entry}})
		if !changed || len(got.Tabs) != 1 || got.Tabs[0].SessionID != sessionID || got.Tabs[0].SessionPath != "" {
			t.Fatalf("recovery pseudo-route evidence = changed:%v file:%+v", changed, got)
		}
		if !got.Tabs[0].restoreBlocked {
			t.Fatal("missing recovery content was allowed to create a replacement runtime")
		}
	})
}
