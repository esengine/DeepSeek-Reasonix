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

const migrationV4 = `
ALTER TABLE catalog_sessions ADD COLUMN recovery_copy INTEGER NOT NULL DEFAULT 0;
`

const migrationV5 = `
ALTER TABLE catalog_sessions ADD COLUMN recovery_group_id TEXT NOT NULL DEFAULT '';
ALTER TABLE catalog_sessions ADD COLUMN recovery_role TEXT NOT NULL DEFAULT '';
ALTER TABLE catalog_sessions ADD COLUMN recovery_canonical INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_catalog_sessions_recovery_group
ON catalog_sessions(recovery_group_id, recovery_role, last_activity_at DESC);
`

const migrationV6 = `
ALTER TABLE catalog_topics ADD COLUMN recovery_branch_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE catalog_topics ADD COLUMN recovery_unresolved_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE catalog_topics ADD COLUMN recovery_cleanup_eligible_count INTEGER NOT NULL DEFAULT 0;
`

const migrationV7 = `
ALTER TABLE catalog_sessions ADD COLUMN logical_topic_id TEXT NOT NULL DEFAULT '';
ALTER TABLE catalog_sessions ADD COLUMN ordinary_visible INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_catalog_sessions_ordinary
ON catalog_sessions(scope, workspace_root, ordinary_visible, last_activity_at DESC);
`

// Folded-topic tombstones remember that lineage projection re-anchored a
// recovery copy's sessions onto the canonical topic. SyncMetadata consults
// them so the desktop topic registry cannot resurrect the copy's pre-reanchor
// topic as a standalone sidebar row after its session rows moved away.
const migrationV8 = `
CREATE TABLE IF NOT EXISTS catalog_folded_topics (
    scope TEXT NOT NULL,
    workspace_root TEXT NOT NULL DEFAULT '',
    topic_id TEXT NOT NULL,
    folded_at INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(scope, workspace_root, topic_id)
);
`

// v9 separates filesystem identity from the original access spelling. The
// production default moves to a fresh v6.sqlite generation together with this
// migration, so old v5 writers never share the new identity columns. Explicit
// legacy catalog paths are invalidated in place: filesystem-aware keys cannot
// be backfilled safely in SQL, and every deleted row is a disposable projection
// that the next authoritative directory reconciliation recreates.
const migrationV9 = `
ALTER TABLE catalog_directories ADD COLUMN path_key TEXT NOT NULL DEFAULT '';
ALTER TABLE catalog_sessions ADD COLUMN path_key TEXT NOT NULL DEFAULT '';
ALTER TABLE catalog_sessions ADD COLUMN directory_key TEXT NOT NULL DEFAULT '';

DELETE FROM catalog_sessions;
DELETE FROM catalog_directories;
DELETE FROM catalog_topics;
DELETE FROM catalog_folded_topics;

CREATE UNIQUE INDEX IF NOT EXISTS idx_catalog_directories_path_key
ON catalog_directories(path_key);
CREATE UNIQUE INDEX IF NOT EXISTS idx_catalog_sessions_path_key
ON catalog_sessions(path_key);
CREATE INDEX IF NOT EXISTS idx_catalog_sessions_directory_key
ON catalog_sessions(directory_key, seen_generation, missing_since);
`

// v10 extends filesystem identity to workspace roots. Access spellings remain
// available for UI/file access, while every relationship and uniqueness rule
// uses the filesystem-aware key. Existing v9 projections are disposable and
// must be rebuilt because SQL cannot infer volume-specific case semantics.
const migrationV10 = `
ALTER TABLE catalog_projects ADD COLUMN workspace_root_key TEXT NOT NULL DEFAULT '';
ALTER TABLE catalog_topics ADD COLUMN workspace_root_key TEXT NOT NULL DEFAULT '';
ALTER TABLE catalog_sessions ADD COLUMN workspace_root_key TEXT NOT NULL DEFAULT '';
ALTER TABLE catalog_folded_topics ADD COLUMN workspace_root_key TEXT NOT NULL DEFAULT '';

DELETE FROM catalog_sessions;
DELETE FROM catalog_directories;
DELETE FROM catalog_projects;
DELETE FROM catalog_topics;
DELETE FROM catalog_folded_topics;

CREATE UNIQUE INDEX IF NOT EXISTS idx_catalog_projects_workspace_key
ON catalog_projects(scope, workspace_root_key);
CREATE UNIQUE INDEX IF NOT EXISTS idx_catalog_topics_workspace_key
ON catalog_topics(scope, workspace_root_key, topic_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_catalog_folded_topics_workspace_key
ON catalog_folded_topics(scope, workspace_root_key, topic_id);
CREATE INDEX IF NOT EXISTS idx_catalog_topics_workspace_page
ON catalog_topics(scope, workspace_root_key, pinned DESC, last_activity_at DESC, topic_id ASC);
CREATE INDEX IF NOT EXISTS idx_catalog_sessions_workspace_topic
ON catalog_sessions(scope, workspace_root_key, topic_id, last_activity_at DESC, path ASC);
CREATE INDEX IF NOT EXISTS idx_catalog_sessions_workspace_history
ON catalog_sessions(scope, workspace_root_key, last_activity_at DESC, path ASC);
CREATE INDEX IF NOT EXISTS idx_catalog_sessions_workspace_ordinary
ON catalog_sessions(scope, workspace_root_key, ordinary_visible, last_activity_at DESC);
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
		{Version: 4, Apply: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, migrationV4)
			return err
		}},
		{Version: 5, Apply: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, migrationV5)
			return err
		}},
		{Version: 6, Apply: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, migrationV6)
			return err
		}},
		{Version: 7, Apply: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, migrationV7)
			return err
		}},
		{Version: 8, Apply: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, migrationV8)
			return err
		}},
		{Version: 9, Apply: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, migrationV9)
			return err
		}},
		{Version: 10, Apply: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, migrationV10)
			return err
		}},
	}
}
