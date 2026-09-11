package taskcatalog

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/projectiondb"
	"reasonix/internal/taskmonitor"
)

// Rebuild replaces only the disposable task projection after replaying the
// authoritative snapshots for the supplied projects into a validated sibling
// database. Event logs remain lazily indexed when a task is expanded.
func Rebuild(ctx context.Context, path string, projects []Project) (Status, error) {
	if path == "" {
		path = DefaultPath()
	}
	if strings.TrimSpace(path) == "" {
		catalog, err := Open(ctx, "")
		if err != nil {
			return Status{}, err
		}
		catalog.reconcileMu.Lock()
		catalog.reconcileDone = true
		catalog.reconcileMu.Unlock()
		if err := reconcileProjects(ctx, catalog, projects); err != nil {
			closeRebuildCatalog(catalog)
			return catalog.Status(), err
		}
		status := catalog.Status()
		closeRebuildCatalog(catalog)
		return status, nil
	}
	err := projectiondb.Rebuild(ctx, projectiondb.OpenOptions{
		Path: path, MemoryName: "task-catalog-rebuild", Migrations: migrations(),
	}, func(ctx context.Context, db *sql.DB) error {
		catalog := &Catalog{
			db: db, store: taskmonitor.NewFileStore(filepath.Join(".reasonix", "tasks")),
			status:      Status{State: "ready", Mode: projectiondb.ModeDisk, Path: path},
			reconciling: map[string]bool{}, registered: map[string]bool{}, reconcileDone: true,
		}
		return reconcileProjects(ctx, catalog, projects)
	})
	if err != nil {
		return Status{}, err
	}
	published, err := Open(ctx, path)
	if err != nil {
		return Status{}, err
	}
	status := published.Status()
	closeRebuildCatalog(published)
	return status, nil
}

func reconcileProjects(ctx context.Context, catalog *Catalog, projects []Project) error {
	for _, input := range projects {
		project, err := catalog.RegisterProject(ctx, input.Root, input.Label)
		if err != nil {
			return err
		}
		if err := catalog.ReconcileProject(ctx, project); err != nil {
			return err
		}
	}
	return nil
}

func normalizeProject(project Project) Project {
	project.Root = filepath.Clean(strings.TrimSpace(project.Root))
	if abs, err := filepath.Abs(project.Root); err == nil {
		project.Root = abs
	}
	project.Key = ProjectKey(project.Root)
	project.Label = strings.TrimSpace(project.Label)
	return project
}

func closeRebuildCatalog(catalog *Catalog) {
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = catalog.Close(closeCtx)
}
