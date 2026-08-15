package main

import (
	"os"
	"sort"

	"reasonix/internal/sessioncatalog"
)

type catalogWorkspaceAvailability struct {
	usable   bool
	complete bool
	ready    int
	pending  int
	failed   int
}

func (availability catalogWorkspaceAvailability) decorate(page ProjectTopicPage, revision uint64) ProjectTopicPage {
	page.Revision = max(page.Revision, revision)
	page.Complete = availability.complete
	page.ReadyDirectories = availability.ready
	page.PendingDirectories = availability.pending
	page.FailedDirectories = availability.failed
	return page
}

func (a *App) catalogWorkspaceAvailability(catalog *sessioncatalog.Catalog, scope, workspaceRoot string) catalogWorkspaceAvailability {
	availability := catalogWorkspaceAvailability{}
	if a == nil || catalog == nil {
		return availability
	}
	ctx, cancel := a.catalogReadContext()
	defer cancel()
	scope, workspaceRoot = normalizeDesktopTopicScope(scope, workspaceRoot)
	for _, target := range a.sessionCatalogTargets() {
		if target.Scope != scope || scope == "project" && !sameProjectRoot(target.WorkspaceRoot, workspaceRoot) {
			continue
		}
		if _, err := os.Stat(target.Path); os.IsNotExist(err) {
			continue
		}
		switch catalog.DirectoryStatus(ctx, target.Path).State {
		case "ready":
			availability.ready++
		case "degraded":
			availability.failed++
		default:
			availability.pending++
		}
	}
	hasRecords := catalog.HasWorkspaceRecords(ctx, scope, workspaceRoot)
	availability.usable = hasRecords || availability.ready > 0
	availability.complete = availability.pending == 0 && availability.failed == 0 && availability.ready > 0
	if availability.ready+availability.pending+availability.failed == 0 && hasRecords {
		availability.complete = true
	}
	return availability
}

func (a *App) mergeMetadataTopics(req ProjectTopicPageRequest, page ProjectTopicPage) ProjectTopicPage {
	metadata := a.metadataTopicPage(req)
	seen := make(map[string]struct{}, len(page.Items)+len(metadata.Items))
	for _, item := range page.Items {
		seen[item.TopicID] = struct{}{}
	}
	for _, item := range metadata.Items {
		if _, exists := seen[item.TopicID]; exists {
			continue
		}
		seen[item.TopicID] = struct{}{}
		page.Items = append(page.Items, item)
	}
	sort.SliceStable(page.Items, func(i, j int) bool {
		if page.Items[i].Pinned != page.Items[j].Pinned {
			return page.Items[i].Pinned
		}
		left := projectTopicSortValue(page.Items[i].CreatedAt, page.Items[i].LastActivityAt, req.SortMode)
		right := projectTopicSortValue(page.Items[j].CreatedAt, page.Items[j].LastActivityAt, req.SortMode)
		if left != right {
			return left > right
		}
		return page.Items[i].TopicID < page.Items[j].TopicID
	})
	limit := req.Limit
	if limit <= 0 {
		limit = sessioncatalog.DefaultLimit
	}
	if limit > sessioncatalog.MaxLimit {
		limit = sessioncatalog.MaxLimit
	}
	hasMore := page.NextCursor != "" || metadata.NextCursor != "" || len(page.Items) > limit
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
	}
	page.NextCursor = ""
	if hasMore && len(page.Items) > 0 {
		page.NextCursor = encodeProjectNodeCursor(page.Items[len(page.Items)-1], req.SortMode)
	}
	return page
}
