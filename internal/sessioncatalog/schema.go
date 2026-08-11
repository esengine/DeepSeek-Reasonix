package sessioncatalog

import (
	"context"
	"database/sql"

	"reasonix/internal/projectiondb"
)

const migrationV1 = `
CREATE TABLE IF NOT EXISTS catalog_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    revision INTEGER NOT NULL DEFAULT 0
);
INSERT OR IGNORE INTO catalog_state(id, revision) VALUES(1, 0);

CREATE TABLE IF NOT EXISTS catalog_directories (
    path TEXT PRIMARY KEY,
    scope TEXT NOT NULL,
    workspace_root TEXT NOT NULL DEFAULT '',
    signature TEXT NOT NULL DEFAULT '',
    scan_cursor TEXT NOT NULL DEFAULT '',
    scan_generation INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL DEFAULT 'pending',
    error TEXT NOT NULL DEFAULT '',
    indexed INTEGER NOT NULL DEFAULT 0,
    total INTEGER NOT NULL DEFAULT 0,
    completed_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS catalog_projects (
    scope TEXT NOT NULL,
    workspace_root TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    color TEXT NOT NULL DEFAULT '',
    pinned INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(scope, workspace_root)
);

CREATE TABLE IF NOT EXISTS catalog_topics (
    scope TEXT NOT NULL,
    workspace_root TEXT NOT NULL DEFAULT '',
    topic_id TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    title_source TEXT NOT NULL DEFAULT '',
    pinned INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    turns INTEGER NOT NULL DEFAULT 0,
    turns_state TEXT NOT NULL DEFAULT 'unknown',
    created_at INTEGER NOT NULL DEFAULT 0,
    last_activity_at INTEGER NOT NULL DEFAULT 0,
    recovery_state TEXT NOT NULL DEFAULT '',
    health TEXT NOT NULL DEFAULT 'ok',
    PRIMARY KEY(scope, workspace_root, topic_id)
);

CREATE TABLE IF NOT EXISTS catalog_sessions (
    path TEXT PRIMARY KEY,
    directory TEXT NOT NULL,
    scope TEXT NOT NULL,
    workspace_root TEXT NOT NULL DEFAULT '',
    topic_id TEXT NOT NULL DEFAULT '',
    topic_title TEXT NOT NULL DEFAULT '',
    custom_title TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL DEFAULT 0,
    last_activity_at INTEGER NOT NULL DEFAULT 0,
    preview TEXT NOT NULL DEFAULT '',
    turns INTEGER NOT NULL DEFAULT 0,
    turns_state TEXT NOT NULL DEFAULT 'unknown',
    recovered INTEGER NOT NULL DEFAULT 0,
    recovery_reason TEXT NOT NULL DEFAULT '',
    recovery_digest TEXT NOT NULL DEFAULT '',
    parent_id TEXT NOT NULL DEFAULT '',
    content_fingerprint TEXT NOT NULL DEFAULT '',
    meta_fingerprint TEXT NOT NULL DEFAULT '',
    health TEXT NOT NULL DEFAULT 'ok',
    missing_since INTEGER NOT NULL DEFAULT 0,
    seen_generation INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_catalog_topics_page
    ON catalog_topics(scope, workspace_root, pinned DESC, last_activity_at DESC, topic_id ASC);
CREATE INDEX IF NOT EXISTS idx_catalog_topics_activity
    ON catalog_topics(last_activity_at DESC, topic_id ASC);
CREATE INDEX IF NOT EXISTS idx_catalog_sessions_topic
    ON catalog_sessions(scope, workspace_root, topic_id, last_activity_at DESC, path ASC);
CREATE INDEX IF NOT EXISTS idx_catalog_sessions_directory
    ON catalog_sessions(directory, seen_generation, missing_since);
CREATE INDEX IF NOT EXISTS idx_catalog_sessions_path
    ON catalog_sessions(path);
`

const migrationV2 = `
ALTER TABLE catalog_topics ADD COLUMN metadata_present INTEGER NOT NULL DEFAULT 0;
`

const migrationV3 = `
CREATE INDEX IF NOT EXISTS idx_catalog_sessions_history
ON catalog_sessions(scope, workspace_root, last_activity_at DESC, path ASC);
`

func sessionMigrations() []projectiondb.Migration {
	return []projectiondb.Migration{
		{Version: 1, Apply: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, migrationV1)
			return err
		}},
		{Version: 2, Apply: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, migrationV2)
			return err
		}},
		{Version: 3, Apply: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, migrationV3)
			return err
		}},
	}
}
