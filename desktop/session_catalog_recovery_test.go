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
	node, ok := app.projectNodeFromCatalogTopic(topic, map[string]catalogRuntimeOverlay{}, sessionOverlays)
	if !ok {
		t.Fatal("mixed topic should stay visible")
	}
	// Parent + open copy remain; idle copy is filtered. Two sessions => children.
	if len(node.Children) != 2 {
		t.Fatalf("children = %d, want parent + open recovery copy", len(node.Children))
	}
	for _, child := range node.Children {
		if child.SessionPath == "/s/copy.jsonl" {
			t.Fatal("idle recovery copy must not appear in project tree children")
		}
	}
}

func TestProjectNodeFromCatalogTopicHidesRecoveryOnlyIdleTopic(t *testing.T) {
	app := &App{tabs: map[string]*WorkspaceTab{}, detachedSessions: map[string]*WorkspaceTab{}}
	topic := sessioncatalog.TopicRecord{
		Scope: "global", TopicID: "only-copy", Title: "Copy", RecoveryState: "recovery_only",
		Sessions: []sessioncatalog.SessionRecord{
			{Path: "/s/only.jsonl", Recovered: true, RecoveryCopy: true, Turns: 2, TurnsState: sessioncatalog.TurnsValid, Health: sessioncatalog.HealthOK},
		},
	}
	if _, ok := app.projectNodeFromCatalogTopic(topic, map[string]catalogRuntimeOverlay{}, map[string]catalogRuntimeOverlay{}); ok {
		t.Fatal("idle recovery-only topic must be hidden from the ordinary tree")
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
	node, ok := app.projectNodeFromCatalogTopic(topic, map[string]catalogRuntimeOverlay{}, map[string]catalogRuntimeOverlay{})
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
	node, ok := app.projectNodeFromCatalogTopic(topic, map[string]catalogRuntimeOverlay{}, map[string]catalogRuntimeOverlay{})
	if !ok {
		t.Fatal("topic with parent should stay visible")
	}
	if len(node.Children) != 0 {
		t.Fatalf("children = %d, want collapsed single effective session", len(node.Children))
	}
}

func TestListProjectTopicsSkipsRecoveryOnlyPages(t *testing.T) {
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
