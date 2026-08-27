package topicstate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestStorePersistsAtomicTopicRecord(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "topic-state-v1.sqlite")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.Update(ctx, "topic-1", func(record *Record) {
		record.Title = "First title"
		record.TitleSource = "manual"
		record.CreatedAtMS = 123
		record.AutoMeta = json.RawMessage(`{"stage":2,"future":"kept"}`)
	})
	if err != nil {
		t.Fatal(err)
	}
	if state.Revision != 1 || state.LegacyPendingRevision != 0 {
		t.Fatalf("state = %+v", state)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	snapshot, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	record := snapshot.Records["topic-1"]
	if record.Title != "First title" || record.TitleSource != "manual" || record.CreatedAtMS != 123 {
		t.Fatalf("record = %+v", record)
	}
	var meta map[string]any
	if err := json.Unmarshal(record.AutoMeta, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["future"] != "kept" {
		t.Fatalf("auto meta = %s", record.AutoMeta)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("database mode = %o, want 600", info.Mode().Perm())
	}
}

func TestStoreLegacyOutboxTracksCommittedRevision(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "topic-state-v1.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	state, err := store.SetLegacyBridge(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !state.LegacyBridge {
		t.Fatal("legacy bridge was not enabled")
	}
	state, err = store.Update(ctx, "topic-1", func(record *Record) { record.Title = "renamed" })
	if err != nil {
		t.Fatal(err)
	}
	if state.LegacyPendingRevision != state.Revision || state.Revision == 0 {
		t.Fatalf("state = %+v", state)
	}
	digests := [4]string{"titles", "sources", "created", "auto"}
	state, err = store.MarkLegacyExported(ctx, state.Revision, digests)
	if err != nil {
		t.Fatal(err)
	}
	if state.LegacyPendingRevision != 0 || state.LegacyExportedRevision != state.Revision {
		t.Fatalf("state = %+v", state)
	}
	if state.LegacyTitlesDigest != "titles" || state.LegacyAutoMetaDigest != "auto" {
		t.Fatalf("digests not retained: %+v", state)
	}
}

func TestReplaceFieldPreservesUnknownAutoMetadata(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "topic-state-v1.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.Update(ctx, "topic-1", func(record *Record) {
		record.Title = "old"
		record.AutoMeta = json.RawMessage(`{"stage":1,"future":{"value":7}}`)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplaceTitles(ctx, map[string]string{"topic-1": "new"}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(snapshot.Records["topic-1"].AutoMeta); got != `{"stage":1,"future":{"value":7}}` {
		t.Fatalf("auto metadata changed: %s", got)
	}
}

func TestMergeMissingTitleIndexDoesNotOverwriteNewerRename(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "topic-state-v1.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.Update(ctx, "topic-1", func(record *Record) {
		record.Title = "New manual title"
		record.TitleSource = "manual"
	}); err != nil {
		t.Fatal(err)
	}
	before, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MergeMissingTitleIndex(ctx,
		map[string]string{"topic-1": "Stale repaired title", "topic-2": "Recovered title"},
		map[string]string{"topic-1": "auto", "topic-2": "manual"}, nil); err != nil {
		t.Fatal(err)
	}
	after, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := after.Records["topic-1"]; got.Title != "New manual title" || got.TitleSource != "manual" {
		t.Fatalf("newer rename was overwritten: %+v", got)
	}
	if got := after.Records["topic-2"]; got.Title != "Recovered title" || got.TitleSource != "manual" {
		t.Fatalf("missing topic was not repaired: %+v", got)
	}
	if after.State.Revision != before.State.Revision+1 {
		t.Fatalf("revision = %d, want %d", after.State.Revision, before.State.Revision+1)
	}
}

func TestStoreRejectsFutureSchemaWithoutChangingFile(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "topic-state-v1.sqlite")
	db, err := sql.Open("sqlite", diskFileDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(2, 0)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Open(ctx, path)
	var future *FutureSchemaError
	if !errors.As(err, &future) {
		t.Fatalf("Open error = %v, want FutureSchemaError", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("future-schema database was modified")
	}
}

func TestStoreConcurrentUpdatesAreSerialized(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "topic-state-v1.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const writers = 12
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := store.Update(ctx, "topic-"+time.Unix(int64(i), 0).UTC().Format("150405"), func(record *Record) {
				record.Title = "title"
			})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Records) != writers || snapshot.State.Revision != writers {
		t.Fatalf("records=%d revision=%d", len(snapshot.Records), snapshot.State.Revision)
	}
}
