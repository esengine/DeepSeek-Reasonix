package historycatalog

import (
	"context"
	"database/sql"

	"reasonix/internal/projectiondb"
)

const migrationV1 = `
CREATE TABLE history_state(
    id INTEGER PRIMARY KEY CHECK(id=1),
    revision INTEGER NOT NULL DEFAULT 0,
    tokenizer_version INTEGER NOT NULL
);
INSERT INTO history_state(id,revision,tokenizer_version) VALUES(1,0,1);

CREATE TABLE history_roots(
    path TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    scope TEXT NOT NULL,
    workspace_root TEXT NOT NULL DEFAULT '',
    signature TEXT NOT NULL DEFAULT '',
    scan_generation INTEGER NOT NULL DEFAULT 0,
    scan_cursor TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'pending',
    error TEXT NOT NULL DEFAULT '',
    indexed INTEGER NOT NULL DEFAULT 0,
    total INTEGER NOT NULL DEFAULT 0,
    completed_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE history_sources(
    path TEXT PRIMARY KEY,
    root TEXT NOT NULL,
    source TEXT NOT NULL,
    scope TEXT NOT NULL,
    workspace_root TEXT NOT NULL DEFAULT '',
    content_revision INTEGER NOT NULL DEFAULT 0,
    content_digest TEXT NOT NULL DEFAULT '',
    content_fingerprint TEXT NOT NULL DEFAULT '',
    meta_fingerprint TEXT NOT NULL DEFAULT '',
    message_count INTEGER NOT NULL DEFAULT 0,
    indexed_message_count INTEGER NOT NULL DEFAULT 0,
    custom_title TEXT NOT NULL DEFAULT '',
    topic_id TEXT NOT NULL DEFAULT '',
    topic_title TEXT NOT NULL DEFAULT '',
    preview TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL DEFAULT 0,
    last_activity_at INTEGER NOT NULL DEFAULT 0,
    health TEXT NOT NULL DEFAULT 'ok',
    missing_since INTEGER NOT NULL DEFAULT 0,
    seen_generation INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE history_documents(
    id INTEGER PRIMARY KEY,
    source_path TEXT NOT NULL REFERENCES history_sources(path) ON DELETE CASCADE,
    message_index INTEGER NOT NULL,
    part_index INTEGER NOT NULL DEFAULT 0,
    role TEXT NOT NULL,
    kind TEXT NOT NULL,
    tool_name TEXT NOT NULL DEFAULT '',
    token_count INTEGER NOT NULL DEFAULT 0,
    UNIQUE(source_path,message_index,kind,part_index)
);

CREATE VIRTUAL TABLE history_fts USING fts5(
    terms,
    content='',
    contentless_delete=1,
    tokenize='unicode61 remove_diacritics 2'
);

CREATE INDEX idx_history_sources_scope_activity
ON history_sources(scope,workspace_root,last_activity_at DESC,path ASC);
CREATE INDEX idx_history_documents_source
ON history_documents(source_path,message_index,part_index,id);
`

func migrations() []projectiondb.Migration {
	return []projectiondb.Migration{{Version: 1, Apply: func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, migrationV1)
		return err
	}}}
}
