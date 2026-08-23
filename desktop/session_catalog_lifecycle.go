package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"reasonix/internal/history"
	"reasonix/internal/sessioncatalog"
	"reasonix/internal/taskcatalog"
)

func (a *App) runSessionCatalog(ctx context.Context, rebuild bool) {
	path := sessioncatalog.DefaultPath()
	freshGeneration := false
	if strings.TrimSpace(path) != "" {
		_, statErr := os.Stat(path)
		freshGeneration = errors.Is(statErr, os.ErrNotExist)
	}
	targets := a.sessionCatalogTargets()
	history.RegisterCatalogRoots(historyCatalogRoots(targets))
	projects := loadProjectsFile()
	taskcatalog.RegisterSharedProject(globalWorkspaceRoot(), projects.GlobalTitle)
	for _, project := range projects.Projects {
		taskcatalog.RegisterSharedProject(project.Root, projectDisplayName(project))
	}
	if rebuild {
		if _, err := sessioncatalog.Rebuild(ctx, path, targets); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("desktop: rebuild session catalog", "err", err)
		}
	}
	catalog, err := sessioncatalog.Open(ctx, sessioncatalog.Options{
		Path: path,
		OnRevision: func(revision uint64, roots []string, reason string) {
			a.emitProjectTreeChangedV2(revision, roots, reason)
		},
	})
	if err != nil {
		slog.Warn("desktop: open session catalog", "err", err)
		return
	}
	if ctx.Err() != nil || a.shuttingDown.Load() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		_ = catalog.Close(closeCtx)
		closeCancel()
		return
	}
	a.sessionCatalog.Store(catalog)
	if err := a.syncSessionCatalogMetadataBounded(ctx, catalog); err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("desktop: sync session catalog metadata", "err", err)
	}
	select {
	case <-a.tabsRestoredSignal():
	case <-ctx.Done():
		return
	}
	// Restored tabs can reveal a project absent from the initial registry.
	targets = a.sessionCatalogTargets()
	history.RegisterCatalogRoots(historyCatalogRoots(targets))
	a.indexRestoredSessionPaths(ctx, catalog)
	// Directory scans are independent and internally batched/resumable; keep
	// startup work bounded so a large project set cannot starve the UI.
	var reconcileGroup errgroup.Group
	reconcileGroup.SetLimit(4)
	for _, target := range targets {
		if ctx.Err() != nil || a.shuttingDown.Load() {
			return
		}
		reconcileGroup.Go(func() error {
			if !rebuild {
				if migrated := migrateLegacySessionsIntoGlobalTopics(target.Path); len(migrated) > 0 {
					_ = a.syncSessionCatalogMetadataBounded(ctx, catalog)
				}
			}
			if err := catalog.ReconcileDirectory(ctx, target); err != nil && !errors.Is(err, context.Canceled) {
				slog.Debug("desktop: reconcile session catalog directory", "dir", target.Path, "err", err)
			}
			return nil
		})
	}
	_ = reconcileGroup.Wait()
	if rebuild {
		catalog.MarkRepairReason("manual_rebuild")
	} else if freshGeneration {
		catalog.MarkRepairReason("generation_upgrade")
	}
	a.retargetOpenTabsToContinuations()
	a.runSessionCatalogRefreshLoop(ctx, catalog, rebuild)
}

func (a *App) runSessionCatalogRefreshLoop(ctx context.Context, catalog *sessioncatalog.Catalog, rebuild bool) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := a.syncSessionCatalogMetadataBounded(ctx, catalog); err != nil && !errors.Is(err, context.Canceled) {
				slog.Debug("desktop: refresh session catalog metadata", "err", err)
			}
			for _, target := range a.sessionCatalogTargets() {
				if !rebuild {
					if migrated := migrateLegacySessionsIntoGlobalTopics(target.Path); len(migrated) > 0 {
						_ = a.syncSessionCatalogMetadataBounded(ctx, catalog)
					}
				}
				catalog.RequestReconcile(target)
				// Count sweep rides the periodic reconcile tick; it only moves
				// provably redundant copies into the recoverable trash.
				a.sweepExcessRecoveryCopies(catalog, target)
			}
		case <-ctx.Done():
			return
		}
	}
}
