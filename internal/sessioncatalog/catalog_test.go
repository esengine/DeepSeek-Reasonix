package sessioncatalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
)

func TestReconcileMakesUnknownCountsVisibleWithoutReadingTranscript(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.jsonl")
	if err := os.WriteFile(path, []byte("not valid jsonl\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{
		Scope:         "project",
		WorkspaceRoot: "/workspace",
		TopicID:       "topic-1",
		TopicTitle:    "Legacy topic",
		SchemaVersion: 1,
		Turns:         0,
	}); err != nil {
		t.Fatal(err)
	}

	catalog, err := Open(ctx, Options{
		Path:          filepath.Join(t.TempDir(), "catalog.sqlite"),
		DisableRepair: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })

	if err := catalog.ReconcileDirectory(ctx, DirectoryTarget{
		Path:          dir,
		Scope:         "project",
		WorkspaceRoot: "/workspace",
	}); err != nil {
		t.Fatal(err)
	}
	page, err := catalog.ListTopics(ctx, TopicPageRequest{
		Scope:         "project",
		WorkspaceRoot: "/workspace",
		Limit:         50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || len(page.Items[0].Sessions) != 1 {
		t.Fatalf("page = %#v, want one visible topic/session", page)
	}
	if got := page.Items[0].Sessions[0].TurnsState; got != TurnsUnknown {
		t.Fatalf("turns state = %q, want %q", got, TurnsUnknown)
	}
}

func TestDirectoryScanReadyOnlyAfterFirstReconcile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "chat.jsonl"), []byte(`{"role":"user","content":"hi"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if catalog.DirectoryScanReady(ctx, dir) {
		t.Fatal("opened catalog must not report the directory ready before the first scan")
	}
	if catalog.HasWorkspaceRecords(ctx, "global", "") {
		t.Fatal("opened catalog must not report workspace records before the first scan")
	}
	if err := catalog.ReconcileDirectory(ctx, DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	if !catalog.DirectoryScanReady(ctx, dir) {
		t.Fatal("directory must be ready after ReconcileDirectory finishes")
	}
	if !catalog.HasWorkspaceRecords(ctx, "global", "") {
		t.Fatal("reconciled directory should report workspace records")
	}
}

func TestListTopicsUsesStableKeysetCursor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })

	base := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	for i, topicID := range []string{"a", "b", "c"} {
		if err := catalog.UpsertSession(ctx, SessionRecord{
			Path:           filepath.Join("/sessions", topicID+".jsonl"),
			Directory:      "/sessions",
			Scope:          "global",
			TopicID:        topicID,
			TopicTitle:     topicID,
			LastActivityAt: base.Add(time.Duration(i) * time.Minute).UnixMilli(),
			Turns:          i + 1,
			TurnsState:     TurnsValid,
			Health:         HealthOK,
		}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("first page = %#v", first)
	}
	second, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" {
		t.Fatalf("second page = %#v", second)
	}
	if first.Items[0].TopicID != "c" || first.Items[1].TopicID != "b" || second.Items[0].TopicID != "a" {
		t.Fatalf("keyset order = %q, %q, %q", first.Items[0].TopicID, first.Items[1].TopicID, second.Items[0].TopicID)
	}
}

func TestListTopicsSortsByCreationTimeAcrossPages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	catalog, err := Open(ctx, Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })

	for _, record := range []SessionRecord{
		{Path: "/sessions/a.jsonl", Directory: "/sessions", Scope: "global", TopicID: "a", TopicTitle: "a", CreatedAt: 300, LastActivityAt: 100, Turns: 1, TurnsState: TurnsValid, Health: HealthOK},
		{Path: "/sessions/b.jsonl", Directory: "/sessions", Scope: "global", TopicID: "b", TopicTitle: "b", CreatedAt: 200, LastActivityAt: 300, Turns: 1, TurnsState: TurnsValid, Health: HealthOK},
		{Path: "/sessions/c.jsonl", Directory: "/sessions", Scope: "global", TopicID: "c", TopicTitle: "c", CreatedAt: 100, LastActivityAt: 200, Turns: 1, TurnsState: TurnsValid, Health: HealthOK},
	} {
		if err := catalog.UpsertSession(ctx, record); err != nil {
			t.Fatal(err)
		}
	}

	first, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 2, SortMode: "created"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 2, SortMode: "created", Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || len(second.Items) != 1 || second.NextCursor != "" {
		t.Fatalf("created pages = first %#v second %#v", first, second)
	}
	if first.Items[0].TopicID != "a" || first.Items[1].TopicID != "b" || second.Items[0].TopicID != "c" {
		t.Fatalf("created order = %q, %q, %q; want a, b, c", first.Items[0].TopicID, first.Items[1].TopicID, second.Items[0].TopicID)
	}
}

func TestGetTopicIsNotLimitedByPageSize(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })

	for i := 0; i <= MaxLimit; i++ {
		topicID := fmt.Sprintf("topic-%03d", i)
		if err := catalog.UpsertSession(ctx, SessionRecord{
			Path: filepath.Join("/sessions", topicID+".jsonl"), Directory: "/sessions",
			Scope: "global", TopicID: topicID, TopicTitle: topicID,
			LastActivityAt: int64(MaxLimit - i), TurnsState: TurnsValid, Health: HealthOK,
		}); err != nil {
			t.Fatal(err)
		}
	}

	topic, ok, err := catalog.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "topic-200"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || topic.TopicID != "topic-200" || len(topic.Sessions) != 1 {
		t.Fatalf("topic = %#v, ok=%v", topic, ok)
	}
}

func TestUnchangedDirectorySignatureSkipsReconcileRevision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{
		Scope: "global", TopicID: "topic", TopicTitle: "Topic",
		SchemaVersion: agent.BranchMetaCountsVersion, Turns: 1,
	}); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	target := DirectoryTarget{Path: dir, Scope: "global"}
	if err := catalog.ReconcileDirectory(ctx, target); err != nil {
		t.Fatal(err)
	}
	revision := catalog.Status().Revision
	if err := catalog.ReconcileDirectory(ctx, target); err != nil {
		t.Fatal(err)
	}
	if got := catalog.Status().Revision; got != revision {
		t.Fatalf("unchanged scan bumped revision: got %d want %d", got, revision)
	}
}

func TestSyncMetadataRemovesOnlyMetadataOnlyTopics(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.SyncMetadata(ctx, nil, []TopicMetadata{
		{Scope: "global", TopicID: "metadata-only", Title: "Metadata"},
		{Scope: "global", TopicID: "with-session", Title: "Session"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.UpsertSession(ctx, SessionRecord{
		Path: "/sessions/with-session.jsonl", Directory: "/sessions", Scope: "global",
		TopicID: "with-session", Turns: 1, TurnsState: TurnsValid, Health: HealthOK,
	}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SyncMetadata(ctx, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := catalog.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "metadata-only"}); err != nil || ok {
		t.Fatalf("metadata-only topic survived removal: ok=%v err=%v", ok, err)
	}
	if _, ok, err := catalog.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "with-session"}); err != nil || !ok {
		t.Fatalf("session-derived topic was removed: ok=%v err=%v", ok, err)
	}
}

func TestSyncMetadataPreservesRepresentativeSessionCustomTitle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	record := SessionRecord{
		Path: "/sessions/titled.jsonl", Directory: "/sessions", Scope: "global",
		TopicID: "titled", TopicTitle: "Original topic", CustomTitle: "Explicit session title",
		LastActivityAt: 2, Turns: 1, TurnsState: TurnsValid, Health: HealthOK,
	}
	if err := catalog.UpsertSession(ctx, record); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SyncMetadata(ctx, nil, []TopicMetadata{{
		Scope: "global", TopicID: "titled", Title: "Changed topic", TitleSource: "auto",
	}}); err != nil {
		t.Fatal(err)
	}
	topic, ok, err := catalog.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "titled"})
	if err != nil || !ok || topic.Title != "Explicit session title" || topic.TitleSource != "manual" {
		t.Fatalf("custom title after metadata sync = %+v, ok=%v, err=%v", topic, ok, err)
	}
	page, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 10})
	if err != nil || len(page.Items) != 1 || page.Items[0].TitleSource != "manual" {
		t.Fatalf("listed custom title source = %+v, err=%v", page.Items, err)
	}
	record.CustomTitle = ""
	record.TopicTitle = "Changed topic"
	if err := catalog.UpsertSession(ctx, record); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SyncMetadata(ctx, nil, []TopicMetadata{{
		Scope: "global", TopicID: "titled", Title: "Changed topic", TitleSource: "auto",
	}}); err != nil {
		t.Fatal(err)
	}
	topic, ok, err = catalog.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "titled"})
	if err != nil || !ok || topic.Title != "Changed topic" || topic.TitleSource != "auto" {
		t.Fatalf("cleared custom title after metadata sync = %+v, ok=%v, err=%v", topic, ok, err)
	}
}

func TestSchemaMigrationLedgerRecordsEveryVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	rows, err := catalog.db.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	versions := []int{}
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			t.Fatal(err)
		}
		versions = append(versions, version)
	}
	if fmt.Sprint(versions) != "[1 2 3 4 5 6 7 8]" {
		t.Fatalf("schema migration ledger = %v", versions)
	}
}

func TestListSessionsUsesRevisionBoundKeysetCursor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	for i, name := range []string{"a", "b", "c"} {
		if err := catalog.UpsertSession(ctx, SessionRecord{
			Path: filepath.Join("/sessions", name+".jsonl"), Directory: "/sessions", Scope: "global",
			TopicID: name, CustomTitle: "title " + name, LastActivityAt: int64(i + 1),
			TurnsState: TurnsValid, Health: HealthOK,
		}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := catalog.ListSessions(ctx, SessionPageRequest{Scope: "all", Limit: 2})
	if err != nil || len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	second, err := catalog.ListSessions(ctx, SessionPageRequest{Scope: "all", Limit: 2, Cursor: first.NextCursor})
	if err != nil || len(second.Items) != 1 || second.Items[0].CustomTitle != "title a" {
		t.Fatalf("second=%#v err=%v", second, err)
	}
	if err := catalog.UpsertSession(ctx, SessionRecord{Path: "/sessions/d.jsonl", Directory: "/sessions", Scope: "global", TopicID: "d", LastActivityAt: 4, TurnsState: TurnsValid, Health: HealthOK}); err != nil {
		t.Fatal(err)
	}
	stale, err := catalog.ListSessions(ctx, SessionPageRequest{Scope: "all", Cursor: first.NextCursor})
	if err != nil || !stale.StaleCursor || len(stale.Items) != 0 {
		t.Fatalf("stale=%#v err=%v", stale, err)
	}
}

func TestDirectWriteDuringScanIsNotMarkedMissing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	catalog, err := Open(ctx, Options{
		Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	target := DirectoryTarget{Path: dir, Scope: "global"}
	generation, _, err := catalog.beginDirectoryScan(ctx, target, "test", now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "late.jsonl")
	if err := catalog.UpsertSession(ctx, SessionRecord{
		Path: path, Directory: dir, Scope: "global", TopicID: "late",
		TurnsState: TurnsUnknown, Health: HealthOK,
	}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.finishDirectoryScan(ctx, target, "test", generation, now.UnixMilli(), 0); err != nil {
		t.Fatal(err)
	}
	topic, ok, err := catalog.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "late"})
	if err != nil || !ok || len(topic.Sessions) != 1 || topic.Sessions[0].Health != HealthOK {
		t.Fatalf("late write was marked missing: topic=%#v ok=%v err=%v", topic, ok, err)
	}
}

func TestSessionTopicMoveRecomputesOldTopic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	record := SessionRecord{
		Path: "/sessions/moved.jsonl", Directory: "/sessions", Scope: "global",
		TopicID: "old", Turns: 1, TurnsState: TurnsValid, Health: HealthOK,
	}
	if err := catalog.UpsertSession(ctx, record); err != nil {
		t.Fatal(err)
	}
	record.TopicID = "new"
	if err := catalog.UpsertSession(ctx, record); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := catalog.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "old"}); err != nil || ok {
		t.Fatalf("old topic survived move: ok=%v err=%v", ok, err)
	}
	if _, ok, err := catalog.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "new"}); err != nil || !ok {
		t.Fatalf("new topic missing after move: ok=%v err=%v", ok, err)
	}
}

func TestWriterQueueCoalescesBySessionPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	catalog, err := Open(ctx, Options{
		Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true, QueueCapacity: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	for turns := 1; turns <= 100; turns++ {
		if ok := catalog.EnqueueSession(SessionRecord{
			Path: "/sessions/coalesced.jsonl", Directory: "/sessions", Scope: "global",
			TopicID: "coalesced", Turns: turns, TurnsState: TurnsValid, Health: HealthOK,
		}); !ok {
			t.Fatalf("same-path update %d was rejected by a one-slot queue", turns)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		topic, ok, err := catalog.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "coalesced"})
		if err != nil {
			t.Fatal(err)
		}
		if ok && topic.Turns == 100 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("coalesced writer did not persist the latest record")
}

func TestRemoveSessionTombstoneHidesTopicBeforeDurableDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	catalog, err := Open(ctx, Options{
		Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	record := SessionRecord{
		Path: path, Directory: dir, Scope: "global", TopicID: "topic_tombstone",
		Turns: 1, TurnsState: TurnsValid, Health: HealthOK, LastActivityAt: time.Now().UnixMilli(),
	}
	if err := catalog.UpsertSession(ctx, record); err != nil {
		t.Fatal(err)
	}
	// Hold the directory lock so durable DELETE blocks while the short caller
	// context expires — the read-visible tombstone must still hide the topic.
	dirLock := catalog.directoryLock(dir)
	dirLock.Lock()
	defer dirLock.Unlock()

	short, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	// RemoveSession should return promptly (overlay path) without waiting for
	// the held directory lock forever.
	done := make(chan error, 1)
	go func() {
		done <- catalog.RemoveSession(short, path, "test_tombstone_overlay")
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RemoveSession: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RemoveSession blocked on directory lock instead of recording tombstone first")
	}
	if page, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 50}); err != nil {
		t.Fatal(err)
	} else {
		for _, item := range page.Items {
			if item.TopicID == "topic_tombstone" {
				t.Fatalf("tombstoned topic still visible in ListTopics: %+v", page.Items)
			}
		}
	}
	if _, ok, err := catalog.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "topic_tombstone"}); err != nil || ok {
		t.Fatalf("GetTopic after tombstone: ok=%v err=%v", ok, err)
	}
	if _, ok, err := catalog.GetSession(ctx, path); err != nil || ok {
		t.Fatalf("GetSession after tombstone: ok=%v err=%v", ok, err)
	}
}

func TestRemoveSessionWinsOverQueuedStaleWriteAndAllowsLaterRecreation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	catalog, err := Open(ctx, Options{
		Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	record := SessionRecord{
		Path: path, Directory: dir, Scope: "global", TopicID: "topic",
		Turns: 1, TurnsState: TurnsValid, Health: HealthOK,
	}
	if err := catalog.UpsertSession(ctx, record); err != nil {
		t.Fatal(err)
	}
	record.Turns = 99
	if !catalog.EnqueueSession(record) {
		t.Fatal("queue stale write")
	}
	if err := catalog.RemoveSession(ctx, path, "test_remove"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, ok, err := catalog.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "topic"}); err != nil || ok {
		t.Fatalf("queued write resurrected removed row: ok=%v err=%v", ok, err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{
		Scope: "global", TopicID: "topic", SchemaVersion: agent.BranchMetaCountsVersion, Turns: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.ReconcileDirectory(ctx, DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	if topic, ok, err := catalog.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "topic"}); err != nil || !ok || topic.Turns != 2 {
		t.Fatalf("new external file did not supersede removal: topic=%#v ok=%v err=%v", topic, ok, err)
	}
}

func TestCloseCancelsCatalogWorkerContext(t *testing.T) {
	catalog, err := Open(context.Background(), Options{
		Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	workerDone := catalog.workerCtx.Done()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := catalog.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-workerDone:
	default:
		t.Fatal("catalog close left worker context active")
	}
}

func BenchmarkListTopicsWarmCatalog10K(b *testing.B) {
	ctx := context.Background()
	catalog, err := Open(ctx, Options{
		Path: filepath.Join(b.TempDir(), "catalog.sqlite"), DisableRepair: true,
	})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = catalog.Close(context.Background()) })
	records := make([]SessionRecord, 10_000)
	for i := range records {
		records[i] = SessionRecord{
			Path: filepath.Join("/sessions", fmt.Sprintf("%05d.jsonl", i)), Directory: "/sessions",
			Scope: "project", WorkspaceRoot: "/workspace", TopicID: fmt.Sprintf("topic-%05d", i),
			Preview: fmt.Sprintf("synthetic session %05d", i), Turns: i%20 + 1,
			TurnsState: TurnsValid, Health: HealthOK, LastActivityAt: int64(i + 1),
		}
	}
	for start := 0; start < len(records); start += 64 {
		end := min(start+64, len(records))
		if err := catalog.upsertSessions(ctx, records[start:end], nil, "benchmark-setup"); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for range b.N {
		page, err := catalog.ListTopics(ctx, TopicPageRequest{
			Scope: "project", WorkspaceRoot: "/workspace", Limit: 50,
		})
		if err != nil || len(page.Items) != 50 {
			b.Fatalf("page len=%d err=%v", len(page.Items), err)
		}
	}
}

func TestMissingSessionRequiresTwoScansAndGraceBeforeRemoval(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{
		Scope: "global", TopicID: "topic", TopicTitle: "Topic",
		SchemaVersion: agent.BranchMetaCountsVersion, Turns: 1,
	}); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, Options{
		Path:          filepath.Join(t.TempDir(), "catalog.sqlite"),
		DisableRepair: true,
		MissingGrace:  time.Minute,
		Now:           func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileDirectory(ctx, DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := catalog.ReconcileDirectory(ctx, DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	if page, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 50}); err != nil || len(page.Items) != 1 {
		t.Fatalf("first missing scan removed row: page=%#v err=%v", page, err)
	}
	now = now.Add(2 * time.Minute)
	if err := catalog.ReconcileDirectory(ctx, DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	if page, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 50}); err != nil || len(page.Items) != 0 {
		t.Fatalf("stale row survived second scan after grace: page=%#v err=%v", page, err)
	}
}

func TestOpenQuarantinesCorruptCatalogAndRebuildsProjection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.sqlite")
	if err := os.WriteFile(path, []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, Options{Path: path, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	status := catalog.Status()
	if status.State != StateReady || status.QuarantinedPath == "" {
		t.Fatalf("status = %#v, want ready catalog with quarantined path", status)
	}
	if _, err := os.Stat(status.QuarantinedPath); err != nil {
		t.Fatalf("quarantined catalog: %v", err)
	}
}

func TestOpenBlankPathUsesMemoryWithoutWritingCWD(t *testing.T) {
	// A blank path (CacheDir unavailable or caller override) must use memory
	// and must not create a relative session-catalog file under cwd.
	wd := t.TempDir()
	t.Chdir(wd)
	catalog, err := Open(context.Background(), Options{Path: "   ", DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	status := catalog.Status()
	if status.Mode != ModeMemory {
		t.Fatalf("status=%#v, want memory mode for blank path", status)
	}
	entries, err := os.ReadDir(wd)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "session-catalog") || strings.HasSuffix(entry.Name(), ".sqlite") {
			t.Fatalf("blank path wrote projection into cwd: %s", entry.Name())
		}
	}
}

func TestRebuildFailureKeepsExistingCatalog(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	catalog, err := Open(ctx, Options{Path: path, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	session := filepath.Join(dir, "keep.jsonl")
	if err := os.WriteFile(session, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := catalog.UpsertSession(ctx, SessionRecord{
		Path: session, Directory: dir, Scope: "global", TopicID: "keep",
		Turns: 1, TurnsState: TurnsValid, Health: HealthOK, Preview: "hello",
	}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	// ListSessionOrder on a regular file fails; Rebuild must keep the old DB.
	fileTarget := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(fileTarget, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Rebuild(ctx, path, []DirectoryTarget{{Path: fileTarget, Scope: "global"}}); err == nil {
		t.Fatal("expected rebuild failure for file path")
	}
	restored, err := Open(ctx, Options{Path: path, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restored.Close(context.Background()) })
	topic, ok, err := restored.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "keep"})
	if err != nil || !ok || len(topic.Sessions) != 1 {
		t.Fatalf("rebuild failure lost catalog: ok=%v topic=%#v err=%v", ok, topic, err)
	}
}

func TestRepairDrainEventuallyCompletesBeyondQueue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	seed, err := Open(ctx, Options{Path: path, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	const total = 8
	for i := range total {
		session := filepath.Join(dir, fmt.Sprintf("%02d.jsonl", i))
		if err := os.WriteFile(session, []byte(`{"role":"user","content":"turn"}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := seed.UpsertSession(ctx, SessionRecord{
			Path: session, Directory: dir, Scope: "global", TopicID: fmt.Sprintf("t%d", i),
			TurnsState: TurnsUnknown, Health: HealthOK, LastActivityAt: int64(i + 1),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := seed.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, Options{Path: path, QueueCapacity: 2, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	deadline := time.Now().Add(15 * time.Second)
	for {
		status := catalog.Status()
		if status.RepairPending == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("repair pending stuck at %d", status.RepairPending)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
