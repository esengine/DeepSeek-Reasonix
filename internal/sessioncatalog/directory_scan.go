package sessioncatalog

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
)

type DirectoryScanStatus struct {
	Known bool
	State string
	Error string
}

// DirectoryStatus exposes only scan completeness, never session contents.
// Desktop uses it to publish a per-workspace partial/complete projection while
// allowing already-indexed rows to remain visible during another directory's
// first scan or failure.
func (c *Catalog) DirectoryStatus(ctx context.Context, path string) DirectoryScanStatus {
	if c == nil || c.db == nil {
		return DirectoryScanStatus{}
	}
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return DirectoryScanStatus{}
	}
	var status DirectoryScanStatus
	err := c.db.QueryRowContext(ctx, `SELECT state,error FROM catalog_directories WHERE path=?`, path).Scan(&status.State, &status.Error)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return DirectoryScanStatus{}
	}
	status.Known = true
	return status
}

// DirectoryScanReady reports whether this directory has finished at least one
// catalog scan. An opened-but-unscanned v4 cache is not ready: ListTopics would
// otherwise treat "no rows yet" as "the user has no conversations".
func (c *Catalog) DirectoryScanReady(ctx context.Context, path string) bool {
	return c.DirectoryStatus(ctx, path).State == "ready"
}

// HasWorkspaceRecords reports whether any non-missing session is already
// projected for this workspace. Used so an in-progress scan that has written
// rows stays authoritative, while a brand-new empty v4 cache does not.
func (c *Catalog) HasWorkspaceRecords(ctx context.Context, scope, workspaceRoot string) bool {
	if c == nil || c.db == nil {
		return false
	}
	scope, workspaceRoot = normalizeScope(scope, workspaceRoot)
	var n int
	err := c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_sessions WHERE scope=? AND workspace_root=? AND missing_since=0`,
		scope, workspaceRoot).Scan(&n)
	return err == nil && n > 0
}
