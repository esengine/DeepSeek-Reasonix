package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/sessioncatalog"
)

func TestSessionMetaFromCatalogPropagatesRecoveryCopy(t *testing.T) {
	meta := sessionMetaFromCatalog(sessioncatalog.SessionRecord{
		Path: "/s/copy.jsonl", Preview: "hello", Turns: 2, TurnsState: sessioncatalog.TurnsValid,
		Recovered: true, RecoveryCopy: true, LastActivityAt: 42,
	}, false, false)
	if !meta.Recovered || !meta.RecoveryCopy {
		t.Fatalf("meta = %+v, want recovered recoveryCopy", meta)
	}
	meta = sessionMetaFromCatalog(sessioncatalog.SessionRecord{
		Path: "/s/adopted.jsonl", Recovered: true, RecoveryCopy: false,
	}, false, false)
	if !meta.Recovered || meta.RecoveryCopy {
		t.Fatalf("meta = %+v, want recovered without recoveryCopy", meta)
	}
}

func TestProjectNodeFromCatalogTopicFiltersIdleRecoveryCopies(t *testing.T) {
	app := &App{tabs: map[string]*WorkspaceTab{}, detachedSessions: map[string]*WorkspaceTab{}}
	topic := sessioncatalog.TopicRecord{
		Scope: "global", TopicID: "t1", Title: "Topic", Turns: 3,
		TurnsState: sessioncatalog.TurnsValid, Health: sessioncatalog.HealthOK,
		LastActivityAt: 100, Sessions: []sessioncatalog.SessionRecord{
			{Path: "/s/parent.jsonl", Turns: 3, TurnsState: sessioncatalog.TurnsValid, Health: sessioncatalog.HealthOK, LastActivityAt: 90},
			{Path: "/s/copy.jsonl", Turns: 3, TurnsState: sessioncatalog.TurnsValid, Health: sessioncatalog.HealthOK,
				Recovered: true, RecoveryCopy: true, LastActivityAt: 100},
			{Path: "/s/open-copy.jsonl", Turns: 1, TurnsState: sessioncatalog.TurnsValid, Health: sessioncatalog.HealthOK,
				Recovered: true, RecoveryCopy: true, LastActivityAt: 95},
		},
	}
	sessionOverlays := map[string]catalogRuntimeOverlay{
		sessionRuntimeKey("/s/open-copy.jsonl"): {open: true},
	}
	node, ok := app.projectNodeFromCatalogTopic(topic, map[string]catalogRuntimeOverlay{}, sessionOverlays, nil)
	if !ok {
		t.Fatal("mixed topic should stay visible")
	}
	// Ordinary list is always one logical row; open recovery only flips Open.
	if len(node.Children) != 0 {
		t.Fatalf("children = %d, want collapsed logical row", len(node.Children))
	}
	if !node.Open {
		t.Fatal("open recovery member must aggregate onto the logical row")
	}
}

func TestProjectNodeFromCatalogTopicShowsNonEmptyRecoveryOnlyTopic(t *testing.T) {
	app := &App{tabs: map[string]*WorkspaceTab{}, detachedSessions: map[string]*WorkspaceTab{}}
	topic := sessioncatalog.TopicRecord{
		Scope: "global", TopicID: "only-copy", Title: "Copy", RecoveryState: "recovery_only",
		Sessions: []sessioncatalog.SessionRecord{
			{Path: "/s/only.jsonl", Recovered: true, RecoveryCopy: true, Turns: 2, TurnsState: sessioncatalog.TurnsValid, Health: sessioncatalog.HealthOK},
		},
	}
	node, ok := app.projectNodeFromCatalogTopic(topic, map[string]catalogRuntimeOverlay{}, map[string]catalogRuntimeOverlay{}, nil)
	if !ok {
		t.Fatal("non-empty recovery-only topic must remain discoverable")
	}
	if !node.Recovered || node.RecoveryState != "recovery_only" || node.SessionPath != "/s/only.jsonl" {
		t.Fatalf("recovery-only node = %+v, want a recoverable logical row", node)
	}
}

func TestProjectNodeFromCatalogTopicMarksCanonicalRecovery(t *testing.T) {
	app := &App{tabs: map[string]*WorkspaceTab{}, detachedSessions: map[string]*WorkspaceTab{}}
	topic := sessioncatalog.TopicRecord{
		Scope: "global", TopicID: "adopted", Title: "Recovered", RecoveryState: "adopted",
		Sessions: []sessioncatalog.SessionRecord{
			{Path: "/s/root.jsonl", Turns: 2, TurnsState: sessioncatalog.TurnsValid, Health: sessioncatalog.HealthOK},
			{Path: "/s/canonical.jsonl", Recovered: true, RecoveryRole: sessioncatalog.RecoveryRoleAdopted,
				RecoveryCanonical: true, Turns: 4, TurnsState: sessioncatalog.TurnsValid, Health: sessioncatalog.HealthOK},
		},
	}
	node, ok := app.projectNodeFromCatalogTopic(topic, map[string]catalogRuntimeOverlay{}, map[string]catalogRuntimeOverlay{}, nil)
	if !ok || !node.Recovered || node.RecoveryState != "adopted" {
		t.Fatalf("canonical recovery node = %+v, visible=%v; want recovered adopted row", node, ok)
	}
}

func TestProjectNodeFromCatalogTopicKeepsPinnedRecoveryOnlyTopic(t *testing.T) {
	app := &App{tabs: map[string]*WorkspaceTab{}, detachedSessions: map[string]*WorkspaceTab{}}
	topic := sessioncatalog.TopicRecord{
		Scope: "global", TopicID: "pinned-copy", Title: "Pinned", Pinned: true, RecoveryState: "recovery_only",
		Sessions: []sessioncatalog.SessionRecord{
			{Path: "/s/pinned.jsonl", Recovered: true, RecoveryCopy: true, Turns: 2, TurnsState: sessioncatalog.TurnsValid, Health: sessioncatalog.HealthOK},
		},
	}
	node, ok := app.projectNodeFromCatalogTopic(topic, map[string]catalogRuntimeOverlay{}, map[string]catalogRuntimeOverlay{}, nil)
	if !ok {
		t.Fatal("pinned recovery-only topic must remain visible")
	}
	// After filtering idle copies nothing is left as children, so single-line topic.
	if len(node.Children) != 0 {
		t.Fatalf("children = %d, want collapsed single-line pinned topic", len(node.Children))
	}
}

func TestProjectNodeFromCatalogTopicCollapsesWhenOnlyOneVisible(t *testing.T) {
	app := &App{tabs: map[string]*WorkspaceTab{}, detachedSessions: map[string]*WorkspaceTab{}}
	topic := sessioncatalog.TopicRecord{
		Scope: "global", TopicID: "collapse", Title: "Collapse", Turns: 2,
		Sessions: []sessioncatalog.SessionRecord{
			{Path: "/s/parent.jsonl", Turns: 2, TurnsState: sessioncatalog.TurnsValid, Health: sessioncatalog.HealthOK},
			{Path: "/s/copy.jsonl", Recovered: true, RecoveryCopy: true, Turns: 2, TurnsState: sessioncatalog.TurnsValid, Health: sessioncatalog.HealthOK},
		},
	}
	node, ok := app.projectNodeFromCatalogTopic(topic, map[string]catalogRuntimeOverlay{}, map[string]catalogRuntimeOverlay{}, nil)
	if !ok {
		t.Fatal("topic with parent should stay visible")
	}
	if len(node.Children) != 0 {
		t.Fatalf("children = %d, want collapsed single effective session", len(node.Children))
	}
}

func TestProjectNodeFromCatalogTopicCollapsesDivergedForksToOne(t *testing.T) {
	app := &App{tabs: map[string]*WorkspaceTab{}, detachedSessions: map[string]*WorkspaceTab{}}
	topic := sessioncatalog.TopicRecord{
		Scope: "global", TopicID: "forks", Title: "WebView", Turns: 5,
		Sessions: []sessioncatalog.SessionRecord{
			{Path: "/s/root.jsonl", Turns: 3, TurnsState: sessioncatalog.TurnsValid, Health: sessioncatalog.HealthOK, LastActivityAt: 10},
			{Path: "/s/fork-a.jsonl", Turns: 5, TurnsState: sessioncatalog.TurnsValid, Health: sessioncatalog.HealthOK,
				Recovered: true, RecoveryRole: sessioncatalog.RecoveryRoleDiverged, ParentID: "root", RecoveryGroupID: "root", LastActivityAt: 30},
			{Path: "/s/fork-b.jsonl", Turns: 4, TurnsState: sessioncatalog.TurnsValid, Health: sessioncatalog.HealthOK,
				Recovered: true, RecoveryRole: sessioncatalog.RecoveryRoleDiverged, ParentID: "root", RecoveryGroupID: "root", LastActivityAt: 40},
		},
	}
	node, ok := app.projectNodeFromCatalogTopic(topic, map[string]catalogRuntimeOverlay{}, map[string]catalogRuntimeOverlay{}, nil)
	if !ok {
		t.Fatal("topic with parent should stay visible")
	}
	// Parent remains; idle diverged forks hide while the normal root exists.
	if len(node.Children) != 0 {
		t.Fatalf("children = %d, want collapsed parent-only row, children=%+v", len(node.Children), node.Children)
	}
	if node.RecoveryCopyCount != 0 {
		t.Fatalf("recoveryCopyCount = %d, want compatibility field hidden from ordinary tree", node.RecoveryCopyCount)
	}
}

func TestProjectNodeFromCatalogTopicHidesFoldedCoveredCopyCount(t *testing.T) {
	app := &App{tabs: map[string]*WorkspaceTab{}, detachedSessions: map[string]*WorkspaceTab{}}
	topic := sessioncatalog.TopicRecord{
		Scope: "global", TopicID: "t1", Title: "Topic", Turns: 3,
		TurnsState: sessioncatalog.TurnsValid, Health: sessioncatalog.HealthOK,
		LastActivityAt: 100, Sessions: []sessioncatalog.SessionRecord{
			{Path: "/s/parent.jsonl", Turns: 3, TurnsState: sessioncatalog.TurnsValid, Health: sessioncatalog.HealthOK, LastActivityAt: 90},
			{Path: "/s/copy-a.jsonl", Turns: 3, TurnsState: sessioncatalog.TurnsValid, Health: sessioncatalog.HealthOK,
				Recovered: true, RecoveryCopy: true, LastActivityAt: 100},
			{Path: "/s/copy-b.jsonl", Turns: 3, TurnsState: sessioncatalog.TurnsValid, Health: sessioncatalog.HealthOK,
				Recovered: true, RecoveryCopy: true, LastActivityAt: 95},
		},
	}
	node, ok := app.projectNodeFromCatalogTopic(topic, map[string]catalogRuntimeOverlay{}, map[string]catalogRuntimeOverlay{}, nil)
	if !ok {
		t.Fatal("topic with parent should stay visible")
	}
	if node.RecoveryCopyCount != 0 {
		t.Fatalf("recoveryCopyCount = %d, want compatibility field hidden from ordinary tree", node.RecoveryCopyCount)
	}
}

func TestProjectNodeFromCatalogTopicHidesOrphanForkTopicsWhenPreferredElsewhere(t *testing.T) {
	app := &App{tabs: map[string]*WorkspaceTab{}, detachedSessions: map[string]*WorkspaceTab{}}
	// Separate topic that only holds a non-preferred diverged fork.
	topic := sessioncatalog.TopicRecord{
		Scope: "global", TopicID: "orphan-fork", Title: "WebView",
		Sessions: []sessioncatalog.SessionRecord{
			{Path: "/s/fork-only.jsonl", Turns: 2, TurnsState: sessioncatalog.TurnsValid, Health: sessioncatalog.HealthOK,
				Recovered: true, RecoveryRole: sessioncatalog.RecoveryRoleDiverged, ParentID: "root", RecoveryGroupID: "root"},
		},
	}
	// Workspace preference already chose the parent path for this lineage.
	preferred := map[string]struct{}{"/s/root.jsonl": {}}
	if _, ok := app.projectNodeFromCatalogTopic(topic, map[string]catalogRuntimeOverlay{}, map[string]catalogRuntimeOverlay{}, preferred); ok {
		t.Fatal("orphan non-preferred recovery topic must stay out of the ordinary tree")
	}
}

func TestListProjectTopicsSkipsRecoveryOnlyPages(t *testing.T) {
	isolateDesktopUserDirs(t)
	ctx := context.Background()
	catalog, err := sessioncatalog.Open(ctx, sessioncatalog.Options{
		Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	base := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	// Two idle recovery-only topics (newer) then one ordinary topic (older).
	for i, topicID := range []string{"copy-b", "copy-a", "real"} {
		record := sessioncatalog.SessionRecord{
			Path: filepath.Join("/s", topicID+".jsonl"), Directory: "/s", Scope: "global",
			TopicID: topicID, TopicTitle: topicID, Turns: 1, TurnsState: sessioncatalog.TurnsValid,
			Health: sessioncatalog.HealthOK, LastActivityAt: base.Add(time.Duration(2-i) * time.Minute).UnixMilli(),
		}
		if topicID != "real" {
			record.Recovered = true
			record.RecoveryCopy = true
		}
		if err := catalog.UpsertSession(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	app := &App{tabs: map[string]*WorkspaceTab{}, detachedSessions: map[string]*WorkspaceTab{}}
	app.sessionCatalog.Store(catalog)
	page, err := app.ListProjectTopics(ProjectTopicPageRequest{Scope: "global", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].TopicID != "real" {
		t.Fatalf("page = %+v, want the ordinary topic after skipping recovery-only pages", page.Items)
	}
}
