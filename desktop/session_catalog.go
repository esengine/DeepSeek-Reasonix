package main

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/history"
	"reasonix/internal/sessioncatalog"
	"reasonix/internal/stats"
	"reasonix/internal/taskcatalog"
)

type SessionCatalogStatus struct {
	State           string `json:"state"`
	Mode            string `json:"mode"`
	Revision        uint64 `json:"revision"`
	Indexed         int64  `json:"indexed"`
	Total           int64  `json:"total"`
	RepairPending   int64  `json:"repairPending"`
	LastError       string `json:"lastError,omitempty"`
	QuarantinedPath string `json:"quarantinedPath,omitempty"`
}

type ProjectTreeSnapshot struct {
	Revision     uint64               `json:"revision"`
	Projects     []ProjectNode        `json:"projects"`
	Catalog      SessionCatalogStatus `json:"catalog"`
	Indexed      int64                `json:"indexed"`
	Total        int64                `json:"total"`
	IndexingDone bool                 `json:"indexingDone"`
}

type ProjectTopicPageRequest struct {
	Scope         string `json:"scope"`
	WorkspaceRoot string `json:"workspaceRoot,omitempty"`
	Cursor        string `json:"cursor,omitempty"`
	Limit         int    `json:"limit,omitempty"`
	Query         string `json:"query,omitempty"`
	TimeFilter    string `json:"timeFilter,omitempty"`
}

type ProjectTopicKey struct {
	Scope         string `json:"scope"`
	WorkspaceRoot string `json:"workspaceRoot,omitempty"`
	TopicID       string `json:"topicId"`
}

type ProjectTopicPage struct {
	Items      []ProjectNode `json:"items"`
	NextCursor string        `json:"nextCursor,omitempty"`
	Revision   uint64        `json:"revision"`
}

type ProjectTreeChangedV2 struct {
	Revision uint64   `json:"revision"`
	Roots    []string `json:"roots"`
	Reason   string   `json:"reason"`
}

func flushDesktopDerivedCatalogs(ctx context.Context) error {
	var first error
	if err := history.FlushSharedCatalog(ctx); err != nil && first == nil {
		first = err
	}
	if err := history.CloseSharedCatalog(ctx); err != nil && first == nil {
		first = err
	}
	if err := stats.CloseUsageCatalogs(ctx); err != nil && first == nil {
		first = err
	}
	if err := taskcatalog.ShutdownShared(ctx); err != nil && first == nil {
		first = err
	}
	return first
}

func sessionCatalogStatus(status sessioncatalog.Status) SessionCatalogStatus {
	return SessionCatalogStatus{
		State:           string(status.State),
		Mode:            string(status.Mode),
		Revision:        status.Revision,
		Indexed:         status.Indexed,
		Total:           status.Total,
		RepairPending:   status.RepairPending,
		LastError:       status.LastError,
		QuarantinedPath: status.QuarantinedPath,
	}
}

func (a *App) currentSessionCatalogStatus() SessionCatalogStatus {
	if a == nil {
		return SessionCatalogStatus{State: string(sessioncatalog.StateDegraded), Mode: string(sessioncatalog.ModeMemory)}
	}
	if a.catalogRebuilding.Load() {
		return SessionCatalogStatus{State: string(sessioncatalog.StateRebuilding)}
	}
	if catalog := a.sessionCatalog.Load(); catalog != nil {
		return sessionCatalogStatus(catalog.Status())
	}
	return SessionCatalogStatus{State: string(sessioncatalog.StateOpening)}
}

func (a *App) startSessionCatalog(rebuild bool) {
	if a == nil || a.shuttingDown.Load() {
		return
	}
	a.catalogLifecycleMu.Lock()
	if a.catalogCancel != nil {
		a.catalogLifecycleMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(a.bootContext())
	done := make(chan struct{})
	a.catalogCancel = cancel
	a.catalogDone = done
	a.catalogRebuilding.Store(rebuild)
	a.catalogLifecycleMu.Unlock()

	go func() {
		defer close(done)
		defer a.catalogRebuilding.Store(false)
		path := sessioncatalog.DefaultPath()
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
		if err := a.syncSessionCatalogMetadata(ctx, catalog); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("desktop: sync session catalog metadata", "err", err)
		}
		select {
		case <-a.tabsRestoredSignal():
		case <-ctx.Done():
			return
		}
		a.indexRestoredSessionPaths(ctx, catalog)
		for _, target := range targets {
			if ctx.Err() != nil || a.shuttingDown.Load() {
				return
			}
			// Legacy assignment is deliberately background-only. It can scan and
			// repair old metadata, but no project-tree or controller request waits.
			if migrated := migrateLegacySessionsIntoGlobalTopics(target.Path); len(migrated) > 0 {
				_ = a.syncSessionCatalogMetadata(ctx, catalog)
			}
			if err := catalog.ReconcileDirectory(ctx, target); err != nil && !errors.Is(err, context.Canceled) {
				slog.Debug("desktop: reconcile session catalog directory", "dir", target.Path, "err", err)
			}
		}
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := a.syncSessionCatalogMetadata(ctx, catalog); err != nil && !errors.Is(err, context.Canceled) {
					slog.Debug("desktop: refresh session catalog metadata", "err", err)
				}
				for _, target := range a.sessionCatalogTargets() {
					if migrated := migrateLegacySessionsIntoGlobalTopics(target.Path); len(migrated) > 0 {
						_ = a.syncSessionCatalogMetadata(ctx, catalog)
					}
					catalog.RequestReconcile(target)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (a *App) stopSessionCatalog(timeout time.Duration) {
	if a == nil {
		return
	}
	a.catalogLifecycleMu.Lock()
	cancel := a.catalogCancel
	done := a.catalogDone
	a.catalogCancel = nil
	a.catalogDone = nil
	a.catalogLifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	catalog := a.sessionCatalog.Swap(nil)
	deadline := time.Now().Add(timeout)
	if catalog != nil {
		remaining := max(time.Until(deadline), 0)
		ctx, closeCancel := context.WithTimeout(context.Background(), remaining)
		_ = catalog.Close(ctx)
		closeCancel()
	}
	if done != nil {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return
		}
		timer := time.NewTimer(remaining)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
		}
	}
}

func (a *App) cancelAllTabBuilds() {
	if a == nil {
		return
	}
	a.mu.Lock()
	for _, tab := range a.tabs {
		a.supersedeTabBuildLocked(tab)
	}
	for _, tab := range a.detachedSessions {
		a.supersedeTabBuildLocked(tab)
	}
	a.mu.Unlock()
}

func (a *App) sessionCatalogTargets() []sessioncatalog.DirectoryTarget {
	f := loadProjectsFile()
	seen := map[string]bool{}
	out := []sessioncatalog.DirectoryTarget{}
	add := func(target sessioncatalog.DirectoryTarget) {
		target.Path = filepath.Clean(strings.TrimSpace(target.Path))
		if target.Path == "." || target.Path == "" || seen[target.Path] {
			return
		}
		seen[target.Path] = true
		out = append(out, target)
	}
	add(sessioncatalog.DirectoryTarget{Path: config.SessionDir(), Scope: "global"})
	add(sessioncatalog.DirectoryTarget{Path: desktopSessionDir(globalWorkspaceRoot()), Scope: "global"})
	for _, project := range f.Projects {
		add(sessioncatalog.DirectoryTarget{Path: desktopSessionDir(project.Root), Scope: "project", WorkspaceRoot: project.Root})
	}
	return out
}

func listCatalogSessionsForDirectory(ctx context.Context, catalog *sessioncatalog.Catalog,
	target sessioncatalog.DirectoryTarget, directory string) ([]sessioncatalog.SessionRecord, error) {
	for range 2 {
		records := []sessioncatalog.SessionRecord{}
		cursor := ""
		for {
			page, err := catalog.ListSessions(ctx, sessioncatalog.SessionPageRequest{Scope: target.Scope,
				WorkspaceRoot: target.WorkspaceRoot, Directory: directory, Cursor: cursor, Limit: sessioncatalog.MaxLimit})
			if err != nil {
				return nil, err
			}
			if page.StaleCursor {
				break
			}
			records = append(records, page.Items...)
			if page.NextCursor == "" {
				return records, nil
			}
			cursor = page.NextCursor
		}
	}
	return []sessioncatalog.SessionRecord{}, nil
}

func (a *App) indexRestoredSessionPaths(ctx context.Context, catalog *sessioncatalog.Catalog) {
	type restored struct {
		target sessioncatalog.DirectoryTarget
		path   string
	}
	a.mu.RLock()
	items := make([]restored, 0, len(a.tabs)+len(a.detachedSessions))
	collect := func(tab *WorkspaceTab) {
		if tab == nil || strings.TrimSpace(tab.SessionPath) == "" {
			return
		}
		items = append(items, restored{
			target: sessioncatalog.DirectoryTarget{Path: filepath.Dir(tab.SessionPath), Scope: tab.Scope, WorkspaceRoot: tab.WorkspaceRoot},
			path:   tab.SessionPath,
		})
	}
	for _, tab := range a.tabs {
		collect(tab)
	}
	for _, tab := range a.detachedSessions {
		collect(tab)
	}
	a.mu.RUnlock()
	for _, item := range items {
		if ctx.Err() != nil {
			return
		}
		_ = catalog.IndexSessionPath(ctx, item.target, item.path)
	}
}

func (a *App) syncSessionCatalogMetadata(ctx context.Context, catalog *sessioncatalog.Catalog) error {
	f := loadProjectsFile()
	deleted := map[string]bool{}
	for _, topicID := range f.DeletedTopics {
		deleted[topicID] = true
	}
	projects := []sessioncatalog.ProjectRecord{{
		Scope: "global", Title: strings.TrimSpace(f.GlobalTitle), Color: normalizeProjectColor(f.GlobalColor),
	}}
	if projects[0].Title == "" {
		projects[0].Title = "Global"
	}
	topics := []sessioncatalog.TopicMetadata{}
	appendTopics := func(scope, root string, ids, pinnedIDs []string) {
		titles := loadTopicTitles(root)
		sources := loadTopicTitleSources(root)
		created := loadTopicCreatedAts(root)
		ordered := pinnedTopicIDs(orderedTopicIDs(ids, titles), pinnedIDs)
		for index, topicID := range ordered {
			if deleted[topicID] {
				continue
			}
			title := strings.TrimSpace(titles[topicID])
			if title == "" {
				title = defaultTopicTitle
			}
			topics = append(topics, sessioncatalog.TopicMetadata{
				Scope: scope, WorkspaceRoot: root, TopicID: topicID, Title: title,
				TitleSource: sources[topicID], Pinned: containsDesktopString(pinnedIDs, topicID),
				SortOrder: index, CreatedAt: topicCreatedAtForTree(created, topicID),
			})
		}
	}
	appendTopics("global", "", f.GlobalTopics, f.GlobalPinnedTopics)
	for index, project := range f.Projects {
		title := strings.TrimSpace(project.Title)
		if title == "" {
			title = workspaceName(project.Root)
		}
		projects = append(projects, sessioncatalog.ProjectRecord{
			Scope: "project", WorkspaceRoot: project.Root, Title: title, Color: project.Color,
			Pinned: containsDesktopString(f.PinnedProjects, project.Root), SortOrder: index,
		})
		appendTopics("project", project.Root, project.Topics, project.PinnedTopics)
	}
	return catalog.SyncMetadata(ctx, projects, topics)
}

func (a *App) emitProjectTreeChangedV2(revision uint64, roots []string, reason string) {
	if roots == nil {
		roots = []string{}
	}
	a.emitRuntimeEvent("project-tree:changed-v2", ProjectTreeChangedV2{Revision: revision, Roots: roots, Reason: reason})
	// One-release compatibility event. Its wrapper is catalog-only, so legacy
	// frontends refresh without reintroducing synchronous history I/O.
	a.emitRuntimeEvent("project-tree:changed")
}

func (a *App) requestSessionCatalogReconcile(dir string) {
	catalog := a.sessionCatalog.Load()
	if catalog == nil || a.shuttingDown.Load() || strings.TrimSpace(dir) == "" {
		return
	}
	clean := filepath.Clean(dir)
	target := sessioncatalog.DirectoryTarget{Path: clean, Scope: "global"}
	for _, candidate := range a.sessionCatalogTargets() {
		if sameDesktopPath(candidate.Path, clean) {
			target = candidate
			break
		}
	}
	go func() {
		// Explicit reconcile bypasses disposable migration markers. Signatures
		// keep periodic passes cheap, but an old CLI or restored backup must
		// never be permanently hidden by a timestamp/content collision.
		migrated, migratedPaths := forceMigrateLegacySessionsIntoGlobalTopicsWithPaths(target.Path)
		if len(migrated) > 0 {
			ctx, cancel := context.WithTimeout(a.bootContext(), 30*time.Second)
			// Publish the exact migrated sessions before the broader metadata
			// projection. On large stores (and especially Windows), the metadata
			// pass can take long enough to defeat this interactive fast path.
			for _, path := range migratedPaths {
				if err := catalog.IndexSessionPath(ctx, target, path); err != nil && !errors.Is(err, context.Canceled) {
					slog.Debug("desktop: index migrated session", "path", path, "err", err)
				}
			}
			_ = a.syncSessionCatalogMetadata(ctx, catalog)
			cancel()
		}
		catalog.RequestReconcile(target)
	}()
}

func sessionDirectoryForPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Dir(path)
}

func (a *App) requestSessionCatalogPath(scope, workspaceRoot, path string) {
	if strings.TrimSpace(path) != "" {
		_ = history.PersistObserver().EnqueueSessionPersist(agent.SessionPersistEvent{Path: path, Rewrite: true})
	}
	catalog := a.sessionCatalog.Load()
	if catalog == nil || a.shuttingDown.Load() || strings.TrimSpace(path) == "" {
		return
	}
	catalog.RequestIndexSession(sessioncatalog.DirectoryTarget{
		Path: sessionDirectoryForPath(path), Scope: scope, WorkspaceRoot: workspaceRoot,
	}, path)
}

func (a *App) removeSessionCatalogPath(path, reason string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	_ = history.PersistObserver().EnqueueSessionPersist(agent.SessionPersistEvent{Path: path, Removed: true})
	catalog := a.sessionCatalog.Load()
	if catalog == nil {
		return
	}
	ctx, cancel := context.WithTimeout(a.bootContext(), 150*time.Millisecond)
	defer cancel()
	if err := catalog.RemoveSession(ctx, path, reason); err != nil && !errors.Is(err, context.Canceled) {
		slog.Debug("desktop: remove session catalog row", "err", err)
	}
}

func (a *App) requestSessionCatalogMetadataSync() {
	catalog := a.sessionCatalog.Load()
	if catalog == nil || a.shuttingDown.Load() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.bootContext(), 5*time.Second)
		defer cancel()
		_ = a.syncSessionCatalogMetadata(ctx, catalog)
	}()
}

func (a *App) GetProjectTreeSnapshot() ProjectTreeSnapshot {
	f := loadProjectsFile()
	projects := []ProjectNode{}
	if strings.TrimSpace(f.GlobalTitle) != "" || len(f.GlobalTopics) > 0 || len(f.Projects) == 0 {
		label := strings.TrimSpace(f.GlobalTitle)
		if label == "" {
			label = "Global"
		}
		projects = append(projects, ProjectNode{
			Key: "global_folder", Kind: "global_folder", Label: label,
			Root: globalWorkspaceRoot(), ProjectColor: normalizeProjectColor(f.GlobalColor),
			Children: []ProjectNode{},
		})
	}
	for _, project := range f.Projects {
		label := strings.TrimSpace(project.Title)
		if label == "" {
			label = workspaceName(project.Root)
		}
		projects = append(projects, ProjectNode{
			Key: "project_" + project.Root, Kind: "project", Label: label,
			Root: project.Root, ProjectColor: project.Color,
			Pinned:   containsDesktopString(f.PinnedProjects, project.Root),
			Children: []ProjectNode{},
		})
	}
	projects = applyPinnedProjectOrder(applyProjectTreeOrder(projects, f.SidebarOrder), f.PinnedProjects)
	status := a.currentSessionCatalogStatus()
	return ProjectTreeSnapshot{
		Revision: status.Revision, Projects: projects, Catalog: status,
		Indexed: status.Indexed, Total: status.Total,
		IndexingDone: status.State == string(sessioncatalog.StateReady) && status.RepairPending == 0,
	}
}
