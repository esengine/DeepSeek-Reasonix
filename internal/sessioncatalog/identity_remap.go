package sessioncatalog

import (
	"context"
	"database/sql"
)

// removeRemappedSessionIdentity removes a projection whose access spelling is
// unchanged but whose filesystem identity moved, for example after a symlink
// retarget. The caller reinserts the current identity in the same transaction.
func removeRemappedSessionIdentity(ctx context.Context, tx *sql.Tx, path, pathKey string) ([]TopicKey, error) {
	rows, err := tx.QueryContext(ctx, `SELECT scope,workspace_root,workspace_root_key,topic_id FROM catalog_sessions
		WHERE path=? AND path_key<>? AND topic_id<>''`, path, pathKey)
	if err != nil {
		return nil, err
	}
	affected := []TopicKey{}
	for rows.Next() {
		var key TopicKey
		if err := rows.Scan(&key.Scope, &key.WorkspaceRoot, &key.workspaceKey, &key.TopicID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		affected = append(affected, key)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM catalog_sessions WHERE path=? AND path_key<>?`, path, pathKey); err != nil {
		return nil, err
	}
	return affected, nil
}

func (c *Catalog) removeRemappedDirectoryIdentity(ctx context.Context, tx *sql.Tx, path, pathKey string) error {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT s.scope,s.workspace_root,s.workspace_root_key,s.topic_id
		FROM catalog_sessions s JOIN catalog_directories d ON d.path_key=s.directory_key
		WHERE d.path=? AND d.path_key<>? AND s.topic_id<>''`, path, pathKey)
	if err != nil {
		return err
	}
	affected := []TopicKey{}
	for rows.Next() {
		var key TopicKey
		if err := rows.Scan(&key.Scope, &key.WorkspaceRoot, &key.workspaceKey, &key.TopicID); err != nil {
			_ = rows.Close()
			return err
		}
		affected = append(affected, key)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM catalog_sessions WHERE directory_key IN (
		SELECT path_key FROM catalog_directories WHERE path=? AND path_key<>?
	)`, path, pathKey); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM catalog_directories WHERE path=? AND path_key<>?`, path, pathKey); err != nil {
		return err
	}
	for _, key := range affected {
		if err := c.recomputeTopic(ctx, tx, key); err != nil {
			return err
		}
	}
	return nil
}

func removeRemappedTopicIdentity(ctx context.Context, tx *sql.Tx, key TopicKey, rootKey string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM catalog_topics
		WHERE scope=? AND workspace_root=? AND topic_id=? AND workspace_root_key<>?`,
		key.Scope, key.WorkspaceRoot, key.TopicID, rootKey)
	return err
}

func removeRemappedFoldedTopicIdentity(ctx context.Context, tx *sql.Tx, key TopicKey, rootKey string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM catalog_folded_topics
		WHERE scope=? AND workspace_root=? AND topic_id=? AND workspace_root_key<>?`,
		key.Scope, key.WorkspaceRoot, key.TopicID, rootKey)
	return err
}
