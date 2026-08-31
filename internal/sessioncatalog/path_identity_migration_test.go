package sessioncatalog

import (
	"context"
	"path/filepath"
	"testing"

	"reasonix/internal/projectiondb"
)

func TestPathIdentityMigrationsInvalidateLegacyProjectionForAuthoritativeRebuild(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	handle, err := projectiondb.Open(ctx, projectiondb.OpenOptions{
		Path: path, MemoryName: "session-catalog-v8-test", Migrations: sessionMigrations()[:8], RequireDisk: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO catalog_directories(path,scope) VALUES('/Sessions','global')`,
		`INSERT INTO catalog_projects(scope,title) VALUES('global','Global')`,
		`INSERT INTO catalog_sessions(path,directory,scope,topic_id) VALUES('/Sessions/Mixed.jsonl','/Sessions','global','topic')`,
		`INSERT INTO catalog_topics(scope,topic_id) VALUES('global','topic')`,
		`INSERT INTO catalog_folded_topics(scope,topic_id) VALUES('global','folded')`,
	} {
		if _, err := handle.DB.ExecContext(ctx, statement); err != nil {
			_ = handle.DB.Close()
			t.Fatal(err)
		}
	}
	if err := handle.DB.Close(); err != nil {
		t.Fatal(err)
	}

	catalog, err := Open(ctx, Options{Path: path, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	for _, table := range []string{"catalog_directories", "catalog_projects", "catalog_sessions", "catalog_topics", "catalog_folded_topics"} {
		var rows int
		if err := catalog.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if rows != 0 {
			t.Fatalf("%s rows after v9 migration = %d, want clean disposable projection", table, rows)
		}
	}
}

func TestMigrationV10InvalidatesPublishedV9Projection(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	handle, err := projectiondb.Open(ctx, projectiondb.OpenOptions{
		Path: path, MemoryName: "session-catalog-v9-test", Migrations: sessionMigrations()[:9], RequireDisk: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO catalog_directories(path,path_key,scope) VALUES('/Sessions','/sessions','global')`,
		`INSERT INTO catalog_projects(scope,title) VALUES('global','Global')`,
		`INSERT INTO catalog_sessions(path,path_key,directory,directory_key,scope,topic_id) VALUES('/Sessions/Mixed.jsonl','/sessions/mixed.jsonl','/Sessions','/sessions','global','topic')`,
		`INSERT INTO catalog_topics(scope,topic_id) VALUES('global','topic')`,
		`INSERT INTO catalog_folded_topics(scope,topic_id) VALUES('global','folded')`,
	} {
		if _, err := handle.DB.ExecContext(ctx, statement); err != nil {
			_ = handle.DB.Close()
			t.Fatal(err)
		}
	}
	if err := handle.DB.Close(); err != nil {
		t.Fatal(err)
	}

	catalog, err := Open(ctx, Options{Path: path, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	for _, table := range []string{"catalog_directories", "catalog_projects", "catalog_sessions", "catalog_topics", "catalog_folded_topics"} {
		var rows int
		if err := catalog.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if rows != 0 {
			t.Fatalf("%s rows after v10 migration = %d, want clean disposable projection", table, rows)
		}
	}
}
