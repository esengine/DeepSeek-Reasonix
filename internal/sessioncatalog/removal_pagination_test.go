package sessioncatalog

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestTombstonesDoNotTruncateSessionOrTopicPagination(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	catalog, err := Open(ctx, Options{
		Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	catalog.pathIdentity = func(path string) string { return "identity:" + filepath.Clean(path) }

	paths := make([]string, 5)
	for i := range paths {
		paths[i] = filepath.Join("/sessions", fmt.Sprintf("page-%d.jsonl", i))
		if err := catalog.UpsertSession(ctx, SessionRecord{
			Path: paths[i], Directory: "/sessions", Scope: "global",
			TopicID: fmt.Sprintf("topic-%d", i), TopicTitle: fmt.Sprintf("Topic %d", i),
			LastActivityAt: int64(5 - i), TurnsState: TurnsValid, Health: HealthOK,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Keep the two newest SQL rows behind the read-visible overlay, as happens
	// while an authoritative archive waits for the durable DELETE worker.
	catalog.removedPaths.Store(catalog.pathKey(paths[0]), uint64(1))
	catalog.removedPaths.Store(catalog.pathKey(paths[1]), uint64(1))

	sessions, err := catalog.ListSessions(ctx, SessionPageRequest{Scope: "all", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions.Items) != 2 || sessions.Items[0].Path != paths[2] || sessions.Items[1].Path != paths[3] || sessions.NextCursor == "" {
		t.Fatalf("first visible session page = %+v", sessions)
	}
	sessionTail, err := catalog.ListSessions(ctx, SessionPageRequest{Scope: "all", Limit: 2, Cursor: sessions.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessionTail.Items) != 1 || sessionTail.Items[0].Path != paths[4] {
		t.Fatalf("session tail = %+v", sessionTail)
	}

	topics, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(topics.Items) != 2 || topics.Items[0].TopicID != "topic-2" || topics.Items[1].TopicID != "topic-3" || topics.NextCursor == "" {
		t.Fatalf("first visible topic page = %+v", topics)
	}
	topicTail, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 2, Cursor: topics.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(topicTail.Items) != 1 || topicTail.Items[0].TopicID != "topic-4" {
		t.Fatalf("topic tail = %+v", topicTail)
	}
}

func TestTopicVisibilityScansPastTombstonedPayloadWindow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	catalog, err := Open(ctx, Options{
		Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	catalog.pathIdentity = func(path string) string { return "identity:" + filepath.Clean(path) }

	paths := make([]string, MaxLimit+1)
	for i := range paths {
		paths[i] = filepath.Join("/sessions", fmt.Sprintf("deep-%03d.jsonl", i))
		if err := catalog.UpsertSession(ctx, SessionRecord{
			Path: paths[i], Directory: "/sessions", Scope: "global", TopicID: "deep",
			LastActivityAt: int64(len(paths) - i), TurnsState: TurnsValid, Health: HealthOK,
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range paths[:MaxLimit] {
		catalog.removedPaths.Store(catalog.pathKey(path), uint64(1))
	}
	topic, ok, err := catalog.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "deep"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || len(topic.Sessions) != 1 || topic.Sessions[0].Path != paths[MaxLimit] {
		t.Fatalf("topic after payload-window tombstones: ok=%v topic=%+v", ok, topic)
	}
}

func TestTombstoneOverlayPublishesRefreshableEventBeforeDurableDelete(t *testing.T) {
	t.Parallel()
	type revisionEvent struct {
		revision uint64
		roots    []string
		reason   string
	}
	events := make(chan revisionEvent, 8)
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	catalog, err := Open(ctx, Options{
		Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true,
		OnRevision: func(revision uint64, roots []string, reason string) {
			events <- revisionEvent{revision: revision, roots: roots, reason: reason}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.UpsertSession(ctx, SessionRecord{
		Path: path, Directory: dir, Scope: "project", WorkspaceRoot: "/workspace",
		TopicID: "topic", LastActivityAt: 1, TurnsState: TurnsValid, Health: HealthOK,
	}); err != nil {
		t.Fatal(err)
	}
	for len(events) > 0 {
		<-events
	}

	dirLock := catalog.directoryLock(dir)
	dirLock.Lock()
	defer dirLock.Unlock()
	before := catalog.revision.Load()
	if err := catalog.RemoveSession(ctx, path, "topic_archived"); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.revision != before || event.reason != "topic_archived" || len(event.roots) != 0 {
			t.Fatalf("overlay event = %+v, want equal revision, archive reason, and all roots", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tombstone overlay did not publish an immediate project-tree event")
	}
}
