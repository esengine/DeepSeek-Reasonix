package sessioncatalog

import "context"

func (c *Catalog) failDirectoryScan(ctx context.Context, path string, scanErr error) {
	c.mutationMu.Lock()
	_, _ = c.db.ExecContext(ctx, `UPDATE catalog_directories SET state='degraded',error=? WHERE path_key=?`, scanErr.Error(), c.pathKey(path))
	c.mutationMu.Unlock()
	c.statusMu.Lock()
	c.status.State = StateDegraded
	c.status.LastError = scanErr.Error()
	c.statusMu.Unlock()
}
