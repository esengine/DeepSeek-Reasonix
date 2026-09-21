package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"time"

	"reasonix/desktop/internal/workspacestate"
	"reasonix/internal/session"
)

type historicalCatalogEntry struct {
	scope string
	node  ProjectNode
}

// Discovery publishes metadata only; ordinary pagination never visits source
// directories or replays a historical log. Refreshes share one bounded worker.
func (a *App) requestHistoricalCatalog() {
	c := &a.historicalImports
	c.mu.Lock()
	c.initialize(a.bootContext())
	if !c.catalogEnabled || c.stopped || a.shuttingDown.Load() || c.discoveryPending || time.Since(c.catalogAt) < 5*time.Second {
		c.mu.Unlock()
		return
	}
	c.discoveryPending = true
	revision, ctx := c.catalogRevision, c.ctx
	c.workers.Add(1)
	c.mu.Unlock()
	go func() {
		defer c.workers.Done()
		defer func() {
			c.mu.Lock()
			c.discoveryPending = false
			c.mu.Unlock()
		}()
		if _, err := a.listHistoricalSessions(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("desktop: historical catalog discovery incomplete")
		}
		c.mu.Lock()
		changed := c.catalogRevision != revision && !c.stopped
		c.mu.Unlock()
		if changed {
			// Discovery changes only this read projection. It must not invalidate
			// the legacy index and feed its own reads back into directory scans.
			a.emitProjectTreeChangedEvent()
		}
	}()
}

func (a *App) listHistoricalSessions(ctx context.Context) (HistoricalImportStatus, error) {
	c := &a.historicalImports
	c.discoveryMu.Lock()
	defer c.discoveryMu.Unlock()
	// Failed reads also settle the refresh interval. Otherwise a renderer read
	// can immediately re-admit the same failed discovery.
	defer func() {
		c.mu.Lock()
		c.catalogAt = time.Now()
		c.mu.Unlock()
	}()
	state, err := a.workspaceRegistry().Load(ctx)
	if err != nil {
		return HistoricalImportStatus{Items: []HistoricalSessionView{}}, err
	}
	sources := map[string]historicalSource{}
	add := func(path, format, scope, root, head string) {
		sources[desktopSourceKey(path, head)] = historicalSource{path: path, format: format, scope: scope, root: root, head: head}
	}
	canonical, legacy := a.desktopHistoricalRoots()
	var joined error
	for _, source := range canonical {
		joined = errors.Join(joined, scanHistoricalRoot(ctx, *source, "canonical", add))
	}
	for _, source := range legacy {
		joined = errors.Join(joined, scanHistoricalRoot(ctx, source, "legacy", add))
	}
	addHistoricalRegistrySources(state, add)
	catalog := readHistoricalCanonicalCatalog(ctx, sources)
	saved, presentationErr := readHistoricalSidecar()
	if err := ctx.Err(); err != nil {
		return HistoricalImportStatus{Items: []HistoricalSessionView{}}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.initialize(ctx)
	if !c.queueLoaded {
		c.loadQueueLocked()
	}
	changed := !reflect.DeepEqual(c.catalog, catalog)
	if changed {
		c.catalog = catalog
	}
	if presentationErr == nil {
		changed = changed || !reflect.DeepEqual(c.presentations, saved.Presentations)
		c.presentations = saved.Presentations
	}
	for id, source := range sources {
		if source.path == "" {
			continue
		}
		c.sources[id] = source
		view := historicalImportView(state, id, source, c.views[id])
		if presentation := c.presentations[id]; presentation.Title != "" {
			view.Title = presentation.Title
		}
		changed = changed || !reflect.DeepEqual(c.views[id], view)
		c.views[id] = view
	}
	if changed {
		c.catalogRevision++
	}
	return c.status(), joined
}

func readHistoricalCanonicalCatalog(ctx context.Context, sources map[string]historicalSource) []historicalCatalogEntry {
	rows := []historicalCatalogEntry{}
	for key, source := range sources {
		if ctx.Err() != nil {
			break
		}
		if source.format != "canonical" || source.version != "" {
			continue
		}
		kind := "global_topic"
		if source.scope == "project" {
			kind = "topic"
		}
		node := ProjectNode{Key: "source_" + key, Kind: kind, Root: source.root, Label: filepath.Base(source.path),
			TopicID: "historical-" + key, Historical: true, SessionPath: source.path, SortOrder: -1,
			TurnsState: "unknown", Health: "metadata_pending", Children: []ProjectNode{},
			Source: &SessionSourceRef{HostID: localDesktopHostID, SourceKey: key, Path: source.path}}
		if info, err := session.NewFilesystemPersistence(filepath.Dir(source.path)).Stat(ctx, filepath.Base(source.path)); err == nil {
			if info.Title != "" {
				node.Label = info.Title
			}
			node.Preview, node.Turns = info.Preview, info.Turns
			node.CreatedAt, node.LastActivityAt = info.CreatedAt.UnixMilli(), info.UpdatedAt.UnixMilli()
			if info.MetadataStatus == session.MetadataReady {
				node.TurnsState, node.Health = "valid", "ok"
			}
		} else if stat, statErr := os.Stat(source.path); statErr == nil {
			node.CreatedAt, node.LastActivityAt = stat.ModTime().UnixMilli(), stat.ModTime().UnixMilli()
			node.Health = "degraded"
		}
		rows = append(rows, historicalCatalogEntry{scope: source.scope, node: node})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].node.Key < rows[j].node.Key })
	return rows
}

func (a *App) historicalCanonicalTopics(scope, root string, state workspacestate.State) []ProjectNode {
	index := workspacestate.NewWorkspaceIndex(state)
	return a.historicalCanonicalTopicsFromProjection(scope, root, state, index)
}

func (a *App) historicalCanonicalTopicsFromProjection(scope, root string, state workspacestate.State, index *workspacestate.WorkspaceIndex) []ProjectNode {
	a.requestHistoricalCatalog()
	c := &a.historicalImports
	c.mu.Lock()
	defer c.mu.Unlock()
	rows := []ProjectNode{}
	for _, entry := range c.catalog {
		if entry.scope != scope {
			continue
		}
		if scope == "project" && !index.SameRoot(entry.node.Root, root) {
			continue
		}
		node := entry.node
		if _, adopted := historicalMappingForSource(state, node.Source.SourceKey); adopted {
			continue
		}
		node.PreparationStatus = "available"
		if view, ok := c.views[node.Source.SourceKey]; ok {
			node.PreparationStatus = view.Status
		}
		rows = append(rows, node)
	}
	return rows
}

func applyHistoricalPresentations(nodes []ProjectNode, saved historicalImportQueueSidecar) {
	for i := range nodes {
		if nodes[i].Source == nil {
			continue
		}
		presentation := saved.Presentations[nodes[i].Source.SourceKey]
		if presentation.Title != "" {
			nodes[i].Label = presentation.Title
		}
		if presentation.Pinned != nil {
			nodes[i].Pinned = *presentation.Pinned
		}
	}
}

// A shell-only read must not create workspaces or migrate organization state.
// Sources without canonical members still need their persisted pin overlays.
func (a *App) historicalPinnedShellsFromProjection(req ProjectTopicPageRequest, state workspacestate.State, index *workspacestate.WorkspaceIndex, legacy desktopProject) ([]ProjectNode, error) {
	adopted := map[string]bool{}
	for _, mapping := range state.SourceMappings {
		adopted["source\x00local\x00"+mapping.SourceKey] = true
		if sourceMappingHasPathAlias(mapping) {
			adopted[sessionRuntimeKey(mapping.Path)] = true
		}
	}
	page, err := a.unadoptedLegacyTopics(req, adopted, nil)
	if err != nil {
		return nil, err
	}
	nodes := append(page.Items, a.historicalCanonicalTopicsFromProjection(req.Scope, req.WorkspaceRoot, state, index)...)
	if saved, err := readHistoricalSidecar(); err == nil {
		applyHistoricalPresentations(nodes, saved)
	}
	workspaceID, _, _ := index.Resolve(req.WorkspaceRoot)
	if req.Scope != "project" {
		workspaceID = workspacestate.GlobalWorkspaceID
	}
	workspace := state.Workspaces[workspaceID]
	org := projectedShellOrganization(workspace, state, nodes, legacy)
	req.pinnedOnly = true
	pins := filterWorkspaceSessionNodes(req, org, state, workspaceID, nodes)
	sort.SliceStable(pins, func(i, j int) bool { return projectTopicLess(pins[i], pins[j], req.SortMode, org.ManualOrderEnabled) })
	return pins, nil
}
