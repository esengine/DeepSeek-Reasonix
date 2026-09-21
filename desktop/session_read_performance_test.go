package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/desktop/internal/workspacestate"
	"reasonix/internal/identitylock"
	"reasonix/internal/sessioncatalog"
)

func TestProjectTreeSnapshotIsReadOnlyAcrossManyUnmigratedWorkspaces(t *testing.T) {
	isolateDesktopUserDirs(t)
	base := t.TempDir()
	app := NewApp()
	t.Cleanup(app.closeSessionServices)
	app.ctx = t.Context()
	app.desktopSessions.root = filepath.Join(base, "by-id")
	app.desktopSessions.workspaceState = workspacestate.NewStore(filepath.Join(base, "workspace-state.json"))

	pinned := true
	for i := range 8 {
		root := filepath.Join(base, fmt.Sprintf("workspace-%02d", i))
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := addProject(root, fmt.Sprintf("Workspace %02d", i)); err != nil {
			t.Fatal(err)
		}
		workspaceID, err := app.ensureDesktopWorkspace(t.Context(), "project", root)
		if err != nil {
			t.Fatal(err)
		}
		sessionID := fmt.Sprintf("session-%02d", i)
		if err := app.workspaceRegistry().AttachSession(t.Context(), "", workspaceID, sessionID, ""); err != nil {
			t.Fatal(err)
		}
		if err := app.workspaceRegistry().EnsureSessionTopic(t.Context(), sessionID, "topic-"+sessionID, sessionID); err != nil {
			t.Fatal(err)
		}
		if err := app.workspaceRegistry().UpdatePresentation(t.Context(), []string{sessionID}, nil, &pinned); err != nil {
			t.Fatal(err)
		}
	}
	state, err := app.workspaceRegistry().Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for id, workspace := range state.Workspaces {
		if workspace.Organization != nil {
			t.Fatalf("fixture workspace %q was already migrated", id)
		}
	}
	before, err := os.ReadFile(app.workspaceRegistry().Path())
	if err != nil {
		t.Fatal(err)
	}

	snapshot := app.GetProjectTreeSnapshot()
	if len(snapshot.Projects) < 8 {
		t.Fatalf("snapshot projects=%d, want at least 8", len(snapshot.Projects))
	}
	pinnedSessions := 0
	for _, project := range snapshot.Projects {
		pinnedSessions += len(project.Children)
	}
	if pinnedSessions != 8 {
		t.Fatalf("snapshot pinned sessions=%d, want 8", pinnedSessions)
	}
	after, err := os.ReadFile(app.workspaceRegistry().Path())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("project-tree read modified the workspace registry")
	}
}

func TestSettledOrganizationReadDoesNotAcquireWriterLock(t *testing.T) {
	app, root, _ := canonicalOrganizationFixture(t, "a", "b")
	if _, _, err := app.ensureSessionOrganization("project", root); err != nil {
		t.Fatal(err)
	}
	release, err := identitylock.Acquire(t.Context(), app.workspaceRegistry().Path()+".lock")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, _, err := app.ensureSessionOrganization("project", root); done <- err }()
	select {
	case err := <-done:
		release()
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		release()
		<-done
		t.Fatal("settled list query waited for the registry writer lock")
	}
}

func TestTopicIndexReusesOrderAndObservesPresentationChanges(t *testing.T) {
	app, root, refs := canonicalOrganizationFixture(t, "a", "b", "c")
	req := ProjectTopicPageRequest{Scope: "project", WorkspaceRoot: root, Limit: 1}
	page, err := app.ListProjectTopics(req)
	if err != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("first page: %+v %v", page, err)
	}
	index := &app.desktopSessions.topicIndex
	index.mu.Lock()
	entries := len(index.entries)
	index.mu.Unlock()
	if entries != 1 {
		t.Fatalf("index entries=%d", entries)
	}
	req.Cursor = page.NextCursor
	second, err := app.ListProjectTopics(req)
	if err != nil || len(second.Items) != 1 {
		t.Fatalf("second page: %+v %v", second, err)
	}
	index.mu.Lock()
	after := len(index.entries)
	index.mu.Unlock()
	if after != entries {
		t.Fatal("pagination rebuilt an unchanged sorted index")
	}
	second.Items[0].Session.SessionID = "caller-owned"
	repeated, err := app.ListProjectTopics(req)
	if err != nil || repeated.Items[0].Session.SessionID == "caller-owned" {
		t.Fatal("page mutation corrupted the index")
	}
	pinned := true
	if err := app.workspaceRegistry().UpdatePresentation(t.Context(), []string{refs["c"].SessionID}, nil, &pinned); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ListProjectTopics(req); err == nil {
		t.Fatal("old cursor survived a changed order")
	}
	req.Cursor, req.pinnedOnly = "", true
	page, err = app.ListProjectTopics(req)
	if err != nil || len(page.Items) != 1 || page.Items[0].Session.SessionID != refs["c"].SessionID {
		t.Fatalf("pin index: %+v %v", page, err)
	}
}

func TestCatalogWatchSettledRootsStayCleanAndUnavailableRootsRetry(t *testing.T) {
	good, missing := t.TempDir(), t.TempDir()
	targets := []sessioncatalog.DirectoryTarget{{Path: good}, {Path: missing}}
	watched, dirty := map[string]bool{good: true}, map[string]bool{}
	current := refreshCatalogWatchTargets(nil, nil, targets, watched, dirty)
	clear(dirty)
	current = refreshCatalogWatchTargets(nil, current, targets, watched, dirty)
	if dirty[good] || !dirty[missing] {
		t.Fatalf("idle watched root scanned or fallback lost: %v", dirty)
	}
	current = refreshCatalogWatchTargets(nil, current, targets[:1], watched, dirty)
	if len(current) != 1 || dirty[missing] {
		t.Fatal("removed target retained maintenance work")
	}
}
