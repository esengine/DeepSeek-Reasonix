package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"reasonix/desktop/internal/workspacestate"
	"reasonix/internal/store"
)

func TestRestoreBlockedIdentityPreservesPinsWithoutSidecars(t *testing.T) {
	app := newSavedTabReconcileTestApp(t)
	finishSavedTabMigration(app)
	path := filepath.Join(t.TempDir(), "session-id:unverified")
	entry := desktopTabEntry{ID: "blocked", Scope: "global", SessionPath: path, Goal: "saved goal", PinnedFiles: []string{"README.md"}}
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
	if tab == nil || tab.StartupErr == "" || tab.Ctrl != nil {
		t.Fatalf("blocked restore published an unsafe runtime: %+v", tab)
	}
	if got := persistedDesktopTabEntry(tab); got.SessionPath != path || got.Goal != entry.Goal || !reflect.DeepEqual(got.PinnedFiles, entry.PinnedFiles) {
		t.Fatalf("blocked restore lost saved input: %+v", got)
	}
	artifacts, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(artifacts) != 0 {
		t.Fatalf("blocked restore created artifacts: %v, %v", artifacts, err)
	}
}

func TestRestorePinnedContextRejectsUnverifiedLocators(t *testing.T) {
	for _, path := range []string{"session-id:valid", "session-id:bad/id", filepath.Join(t.TempDir(), "session-id:unverified")} {
		t.Run(path, func(t *testing.T) {
			tab := &WorkspaceTab{SessionPath: path}
			restoreTabPinnedContext(tab, []string{"legacy.md"})
			if got := tab.pendingLegacyPinnedFilesForPersistence(); !reflect.DeepEqual(got, []string{"legacy.md"}) {
				t.Fatalf("unverified input was consumed: %v", got)
			}
			if _, err := os.Stat(store.SessionPinnedContext(path)); err == nil {
				t.Fatal("unverified locator created pinned sidecar")
			}
		})
	}
}

func TestReconcileSavedTabsRejectsPendingOperationIdentityConflicts(t *testing.T) {
	for _, pseudo := range []bool{false, true} {
		for _, mapping := range []bool{false, true} {
			name := "exact"
			if pseudo {
				name = "pseudo"
			}
			if mapping {
				name += "-mapping"
			}
			t.Run(name, func(t *testing.T) {
				app := newSavedTabReconcileTestApp(t)
				root := t.TempDir()
				ref, workspaceID := createLegacyCleanupSession(t, app, root, "session-A", true)
				op := workspacestate.Operation{ID: "operation", Kind: "import", Lifecycle: workspacestate.Active, WorkspaceID: workspaceID, SessionIDs: []string{"session-B"}}
				if mapping {
					op.SessionIDs = []string{ref.SessionID}
					op.Mapping = &workspacestate.SourceMapping{SessionID: "session-B"}
				}
				if err := app.workspaceRegistry().BeginOperation(t.Context(), op); err != nil {
					t.Fatal(err)
				}
				before, err := app.workspaceRegistry().Load(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				finishSavedTabMigration(app)
				entry := savedProjectTab("conflict", "", op.ID, root, workspaceID)
				entry.SessionPath = sessionRoute(ref.SessionID)
				if pseudo {
					entry.SessionPath = `C:\old\` + entry.SessionPath
				}
				got, changed := app.reconcileSavedTabs(t.Context(), desktopTabsFile{Tabs: []desktopTabEntry{entry}})
				if changed || len(got.Tabs) != 1 || !got.Tabs[0].restoreBlocked {
					t.Fatalf("pending conflict was accepted: changed=%v tabs=%+v", changed, got.Tabs)
				}
				if got.Tabs[0].SessionID != entry.SessionID || got.Tabs[0].SessionPath != entry.SessionPath || got.Tabs[0].CreateOperationID != op.ID {
					t.Fatalf("pending conflict changed saved identity: %+v", got.Tabs[0])
				}
				after, err := app.workspaceRegistry().Load(t.Context())
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatalf("conflict mutated registry: %v", err)
				}
			})
		}
	}
}

func TestReconcileSavedTabsAcceptsMatchingPendingOperationIdentity(t *testing.T) {
	app := newSavedTabReconcileTestApp(t)
	root := t.TempDir()
	ref, workspaceID := createLegacyCleanupSession(t, app, root, "session-A", true)
	op := workspacestate.Operation{ID: "operation", Kind: "import", Lifecycle: workspacestate.Active, WorkspaceID: workspaceID, SessionIDs: []string{ref.SessionID}}
	if err := app.workspaceRegistry().BeginOperation(t.Context(), op); err != nil {
		t.Fatal(err)
	}
	finishSavedTabMigration(app)
	entry := savedProjectTab("matching", "", op.ID, root, workspaceID)
	entry.SessionPath = sessionRoute(ref.SessionID)
	got, changed := app.reconcileSavedTabs(t.Context(), desktopTabsFile{Tabs: []desktopTabEntry{entry}})
	if !changed || len(got.Tabs) != 1 || got.Tabs[0].restoreBlocked || got.Tabs[0].SessionID != ref.SessionID || got.Tabs[0].SessionPath != "" {
		t.Fatalf("matching pending identity was rejected: changed=%v tabs=%+v", changed, got.Tabs)
	}
}

func TestHiddenTabPruneRejectsCanonicalIdentityConflict(t *testing.T) {
	app := newSavedTabReconcileTestApp(t)
	root := t.TempDir()
	ref, workspaceID := createLegacyCleanupSession(t, app, root, "session-A", true)
	before, err := app.workspaceRegistry().Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"session-id:session-B", "session-id:bad/id", filepath.Join(t.TempDir(), "session-id:session-B"), filepath.Join(t.TempDir(), "legacy.jsonl")} {
		t.Run(path, func(t *testing.T) {
			tab := &WorkspaceTab{ID: "conflict", Scope: "project", WorkspaceRoot: root, SessionWorkspace: desktopTabWorkspace{ID: workspaceID}, SessionID: ref.SessionID, SessionPath: path}
			app.tabs[tab.ID] = tab
			for _, save := range []func() error{
				func() error { return app.persistHiddenTabBeforePrune(tab.ID, tab) },
				func() error { return app.saveTabSessionMeta(tab, sessionRoute(ref.SessionID)) },
			} {
				var identityErr *sessionLocatorError
				if err := save(); !errors.As(err, &identityErr) {
					t.Fatalf("conflicting identity accepted: %v", err)
				}
			}
			if tab.SessionID != ref.SessionID || tab.SessionPath != path {
				t.Fatal("failed persistence changed tab identity")
			}
		})
	}
	after, err := app.workspaceRegistry().Load(t.Context())
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("failed prune mutated registry: %v", err)
	}
}
