package sessioncatalog

// Guards for connection-pool starvation: every catalog API must finish on a
// pool of one, so no path can hold a connection while waiting for another.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/agent"
)

// A memory-mode catalog pools a single connection, so hydrating a topic's
// sessions from inside the open topic cursor deadlocks until the caller's
// context expires — with the desktop's boot context, forever.
func TestListTopicsHydratesSessionsOffTheTopicCursor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	catalog, err := Open(ctx, Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	for _, topicID := range []string{"topic-a", "topic-b"} {
		if err := catalog.UpsertSession(ctx, SessionRecord{
			Path: filepath.Join("/s", topicID+".jsonl"), Directory: "/s", Scope: "global",
			TopicID: topicID, TopicTitle: topicID, Turns: 1, TurnsState: TurnsValid,
			Health: HealthOK, LastActivityAt: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// A regression cannot hang the suite: it starves the pool and this deadline
	// turns the deadlock into a failed call.
	listCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	page, err := catalog.ListTopics(listCtx, TopicPageRequest{Scope: "global", Limit: MaxLimit})
	if err != nil {
		t.Fatalf("ListTopics on a single-connection catalog: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("topics = %d, want both hydrated topics", len(page.Items))
	}
	for _, item := range page.Items {
		if len(item.Sessions) != 1 {
			t.Fatalf("topic %q sessions = %d, want 1", item.TopicID, len(item.Sessions))
		}
	}
}

// The disk pool is four connections and every in-flight ListTopics pins one for
// its topic cursor, so four concurrent sidebar reads used to leave the nested
// session queries with nothing left to acquire and no deadline to break out of.
func TestListTopicsSurvivesReadersAtThePoolLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	for i := range 50 {
		topicID := fmt.Sprintf("topic-%02d", i)
		if err := catalog.UpsertSession(ctx, SessionRecord{
			Path: filepath.Join("/s", topicID+".jsonl"), Directory: "/s", Scope: "global",
			TopicID: topicID, TopicTitle: topicID, Turns: 1, TurnsState: TurnsValid,
			Health: HealthOK, LastActivityAt: int64(i + 1),
		}); err != nil {
			t.Fatal(err)
		}
	}

	listCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	const readers = 8
	results := make(chan error, readers)
	for range readers {
		go func() {
			page, err := catalog.ListTopics(listCtx, TopicPageRequest{Scope: "global", Limit: MaxLimit})
			if err == nil && len(page.Items) != 50 {
				err = fmt.Errorf("topics = %d, want 50", len(page.Items))
			}
			results <- err
		}()
	}
	for range readers {
		if err := <-results; err != nil {
			t.Fatalf("concurrent ListTopics: %v", err)
		}
	}
}

// A memory-mode catalog pools exactly one connection, so any API that holds a
// connection while asking the pool for another deadlocks here and nowhere else
// until production saturates a disk pool. This is the general guard: it fails
// on the API that reintroduces the nesting, not on ListTopics specifically.
func TestCatalogAPIsSurviveASingleConnectionPool(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hi"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{
		Scope: "project", WorkspaceRoot: "/workspace", TopicID: "topic-1",
		TopicTitle: "Topic", SchemaVersion: agent.BranchMetaCountsVersion, Turns: 1,
	}); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	target := DirectoryTarget{Path: dir, Scope: "project", WorkspaceRoot: "/workspace"}
	key := TopicKey{Scope: "project", WorkspaceRoot: "/workspace", TopicID: "topic-1"}

	// One deadline across the whole surface: a regression surfaces as this
	// step's error instead of a hung test binary.
	call, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	steps := []struct {
		name string
		run  func() error
	}{
		{"ReconcileDirectory", func() error { return catalog.ReconcileDirectory(call, target) }},
		{"IndexSessionPath", func() error { return catalog.IndexSessionPath(call, target, path) }},
		{"SyncMetadata", func() error {
			return catalog.SyncMetadata(call,
				[]ProjectRecord{{Scope: "project", WorkspaceRoot: "/workspace", Title: "W"}},
				[]TopicMetadata{{Scope: "project", WorkspaceRoot: "/workspace", TopicID: "topic-1", Title: "Topic"}})
		}},
		{"ListTopics", func() error {
			page, err := catalog.ListTopics(call, TopicPageRequest{Scope: "project", WorkspaceRoot: "/workspace", Limit: MaxLimit})
			if err == nil && len(page.Items) != 1 {
				return fmt.Errorf("topics = %d, want 1", len(page.Items))
			}
			return err
		}},
		{"GetTopic", func() error {
			topic, ok, err := catalog.GetTopic(call, key)
			if err == nil && (!ok || len(topic.Sessions) != 1) {
				return fmt.Errorf("topic ok=%v sessions=%d, want one session", ok, len(topic.Sessions))
			}
			return err
		}},
		{"ListSessions", func() error {
			page, err := catalog.ListSessions(call, SessionPageRequest{Scope: "project", WorkspaceRoot: "/workspace", Limit: MaxLimit})
			if err == nil && len(page.Items) != 1 {
				return fmt.Errorf("sessions = %d, want 1", len(page.Items))
			}
			return err
		}},
		{"GetSession", func() error {
			_, ok, err := catalog.GetSession(call, path)
			if err != nil {
				return fmt.Errorf("get session: %w", err)
			}
			if !ok {
				return fmt.Errorf("session %q missing from catalog", path)
			}
			return nil
		}},
		{"RemoveSession", func() error { return catalog.RemoveSession(call, path, "test") }},
	}
	for _, step := range steps {
		if err := step.run(); err != nil {
			t.Fatalf("%s on a single-connection catalog: %v", step.name, err)
		}
	}
}
