package main

import (
	"context"
	"path/filepath"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/sessioncatalog"
	"sort"
	"strings"
	"time"
)

// Sidebar reads are bound so a starved connection pool or a slow projection
// degrades into a stale page the next revision repairs, instead of a Wails call
// that never returns and a tree that never moves again.
const sessionCatalogReadTimeout = 10 * time.Second

func (a *App) catalogReadContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(a.bootContext(), sessionCatalogReadTimeout)
}

type catalogRuntimeSnapshot struct {
	tabID            string
	scope            string
	workspaceRoot    string
	topicID          string
	sessionPath      string
	activity         string
	topicTitle       string
	topicTitleSource string
	ctrl             control.SessionAPI
	state            *event.RuntimeStateSnapshot
	open             bool
}

type catalogRuntimeOverlay struct {
	open    bool
	running bool
	status  string
}

func catalogRuntimeStatus(activity string, runtimeStatus control.RuntimeStatus) string {
	status := normalizeTopicStatus(activity)
	if runtimeStatus.PendingPrompt {
		return topicStatusWaitingConfirmation
	}
	if runtimeStatus.Running {
		if status == "" || status == topicStatusError || status == topicStatusPaused || status == topicStatusAwaitingDelivery {
			return topicStatusThinking
		}
		return status
	}
	if runtimeStatus.BackgroundJobs > 0 {
		return topicStatusBackgroundJob
	}
	if status == topicStatusError || status == topicStatusPaused || status == topicStatusAwaitingDelivery {
		return status
	}
	return status
}

func (a *App) catalogRuntimeOverlays() (map[string]catalogRuntimeOverlay, map[string]catalogRuntimeOverlay) {
	topics := map[string]catalogRuntimeOverlay{}
	sessions := map[string]catalogRuntimeOverlay{}
	for _, snap := range a.catalogRuntimeSnapshots() {
		path := strings.TrimSpace(snap.sessionPath)
		if snap.ctrl != nil {
			if path == "" {
				path = snap.ctrl.SessionPath()
			}
		}
		status, running := catalogControllerStatus(snap.ctrl, snap.activity)
		overlay := catalogRuntimeOverlay{open: snap.open, running: running, status: status}
		key := topicSummaryKey(snap.scope, snap.workspaceRoot, snap.topicID)
		current := topics[key]
		current.open = current.open || overlay.open
		current.running = current.running || overlay.running
		if current.status == "" {
			current.status = overlay.status
		}
		topics[key] = current
		if path != "" {
			sessions[sessionRuntimeKey(path)] = overlay
		}
	}
	return topics, sessions
}

func (a *App) metadataProjectTopics(scope, workspaceRoot string) []ProjectNode {
	f := loadProjectsFile()
	deleted := map[string]bool{}
	for _, topicID := range f.DeletedTopics {
		deleted[topicID] = true
	}
	if state, err := a.workspaceRegistry().Load(a.bootContext()); err == nil {
		for topicID := range purgedCanonicalTopicIDs(state) {
			deleted[topicID] = true
		}
	}
	ids := f.GlobalTopics
	pinnedIDs := f.GlobalPinnedTopics
	manualOrder := f.GlobalManualTopicOrder
	titleRoot := ""
	projectColor := normalizeProjectColor(f.GlobalColor)
	if scope == "project" {
		ids = nil
		pinnedIDs = nil
		manualOrder = false
		titleRoot = workspaceRoot
		for _, project := range f.Projects {
			if sameProjectRoot(project.Root, workspaceRoot) {
				ids = project.Topics
				pinnedIDs = project.PinnedTopics
				manualOrder = project.ManualTopicOrder
				projectColor = project.Color
				break
			}
		}
	}
	titles := loadTopicTitles(titleRoot)
	sources := loadTopicTitleSources(titleRoot)
	created := loadTopicCreatedAts(titleRoot)
	topicOverlays, _ := a.catalogRuntimeOverlays()
	runtimeNodes := a.runtimeOnlyProjectTopics(scope, workspaceRoot)
	runtimeByTopic := map[string][]ProjectNode{}
	for _, node := range runtimeNodes {
		runtimeByTopic[node.TopicID] = append(runtimeByTopic[node.TopicID], node)
	}
	out := []ProjectNode{}
	seen := map[string]bool{}
	for sortOrder, topicID := range pinnedTopicIDs(orderedTopicIDs(ids, titles), pinnedIDs) {
		if !manualOrder {
			sortOrder = -1
		}
		if deleted[topicID] {
			continue
		}
		seen[topicID] = true
		title := strings.TrimSpace(titles[topicID])
		if title == "" {
			title = defaultTopicTitle
		}
		kind := "topic"
		if scope != "project" {
			kind = "global_topic"
		}
		overlay := topicOverlays[topicSummaryKey(scope, workspaceRoot, topicID)]
		node := ProjectNode{
			Key: kind + "_" + topicID, Kind: kind,
			Label: a.localizedTopicTitle(title, sources[topicID]), Root: workspaceRoot,
			TopicID: topicID, ProjectColor: projectColor,
			CreatedAt: topicCreatedAtForTree(created, topicID), Pinned: containsDesktopString(pinnedIDs, topicID), SortOrder: sortOrder,
			Open: overlay.open, Running: overlay.running, Status: overlay.status,
			TurnsState: string(sessioncatalog.TurnsUnknown), Health: string(sessioncatalog.HealthOK),
			Children: []ProjectNode{},
		}
		if runtimeRows := runtimeByTopic[topicID]; len(runtimeRows) > 0 {
			for _, runtimeNode := range runtimeRows {
				runtimeNode.Pinned, runtimeNode.SortOrder = node.Pinned, node.SortOrder
				runtimeNode.CreatedAt, runtimeNode.ProjectColor = node.CreatedAt, node.ProjectColor
				if strings.TrimSpace(runtimeNode.Label) == "" {
					runtimeNode.Label = node.Label
				}
				out = append(out, runtimeNode)
			}
			continue
		}
		out = append(out, node)
	}
	for _, runtimeNode := range runtimeNodes {
		if seen[runtimeNode.TopicID] || deleted[runtimeNode.TopicID] {
			continue
		}
		runtimeNode.RuntimeOnly = true
		out = append(out, runtimeNode)
	}
	return out
}

func (a *App) runtimeOnlyProjectTopics(scope, workspaceRoot string) []ProjectNode {
	nodes, _ := a.runtimeOnlyProjectTopicsWithSessions(scope, workspaceRoot)
	return nodes
}

// runtimeOnlyProjectTopicsWithSessions also reports each runtime topic's known
// session paths so callers can resolve the topics those sessions project onto
// in the catalog (a restored tab may carry a legacy topic ID for a re-anchored
// recovery lineage).
func (a *App) runtimeOnlyProjectTopicsWithSessions(scope, workspaceRoot string) ([]ProjectNode, map[string][]string) {
	snapshots := []catalogRuntimeSnapshot{}
	for _, snapshot := range a.catalogRuntimeSnapshots() {
		if scope == "project" {
			if snapshot.scope != "project" || !sameProjectRoot(snapshot.workspaceRoot, workspaceRoot) {
				continue
			}
		} else if snapshot.scope == "project" {
			continue
		}
		snapshots = append(snapshots, snapshot)
	}
	return a.runtimeProjectTopicNodes(scope, workspaceRoot, snapshots, true)
}

func (a *App) runtimeProjectTopicNodes(scope, workspaceRoot string, snapshots []catalogRuntimeSnapshot, previews bool) ([]ProjectNode, map[string][]string) {
	sessionsByTopic := map[string][]string{}
	out := []ProjectNode{}
	kind := "topic"
	if scope != "project" {
		kind = "global_topic"
	}
	for _, snapshot := range snapshots {
		if snapshot.sessionPath == "" && snapshot.ctrl != nil {
			snapshot.sessionPath = snapshot.ctrl.SessionPath()
		}
		path := strings.TrimSpace(snapshot.sessionPath)
		if path != "" {
			sessionsByTopic[snapshot.topicID] = append(sessionsByTopic[snapshot.topicID], path)
		}
		label := defaultTopicTitle
		if strings.TrimSpace(snapshot.topicTitle) != "" {
			label = snapshot.topicTitle
		}
		// A canonical route is an internal identity, never a display name. Only
		// legacy file-backed sessions may use their filename as the last-resort
		// runtime label while the catalog is catching up.
		if _, canonical := parseSessionRoute(path); !canonical {
			if pathLabel := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)); pathLabel != "" && pathLabel != "." {
				label = pathLabel
			}
		}
		var status string
		var running bool
		if snapshot.state != nil {
			status, running = catalogStateStatus(*snapshot.state, snapshot.activity)
		} else {
			status, running = catalogControllerStatus(snapshot.ctrl, snapshot.activity)
		}
		preview := ""
		if previews {
			preview = sessionPreviewForPath(path)
		}
		key := kind + "_" + snapshot.topicID
		if path != "" {
			key = projectSessionNodeKey(scope, path)
		}
		out = append(out, ProjectNode{
			Key: key, Kind: kind, Label: a.localizedTopicTitle(label, snapshot.topicTitleSource),
			Root: workspaceRoot, TopicID: snapshot.topicID, SessionPath: path, Preview: preview,
			Open: snapshot.open, Running: running, Status: status,
			TurnsState: string(sessioncatalog.TurnsUnknown), Health: string(sessioncatalog.HealthOK),
			Children: []ProjectNode{},
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, sessionsByTopic
}

func (a *App) projectNodeFromCatalogTopic(topic sessioncatalog.TopicRecord, topicOverlays, sessionOverlays map[string]catalogRuntimeOverlay, preferred map[string]struct{}) (ProjectNode, bool) {
	kind := "topic"
	if topic.Scope == "global" {
		kind = "global_topic"
	}
	recoveryOnly := topic.RecoveryState == "recovery_only" && recoveryOnlyHasContent(topic.Sessions)
	canonicalRecovery := topic.RecoveryState == "adopted" || topic.RecoveryState == "preferred"
	recoveryState := ""
	recoveryBranchCount := 0
	recoveryUnresolvedCount := 0
	recoveryCleanupEligibleCount := 0
	if recoveryOnly {
		recoveryState = topic.RecoveryState
		recoveryBranchCount = topic.RecoveryBranchCount
		recoveryUnresolvedCount = topic.RecoveryUnresolvedCount
		recoveryCleanupEligibleCount = topic.RecoveryCleanupEligibleCount
	} else if canonicalRecovery {
		recoveryState = topic.RecoveryState
	}
	overlay := topicOverlays[topicSummaryKey(topic.Scope, topic.WorkspaceRoot, topic.TopicID)]
	node := ProjectNode{
		Key: kind + "_" + topic.TopicID, Kind: kind, Label: a.localizedTopicTitle(topic.Title, topic.TitleSource),
		Root: topic.WorkspaceRoot, TopicID: topic.TopicID, Turns: topic.Turns,
		Preview:    topicSessionPreview(topic.Sessions, topic.RepresentativePath),
		TurnsState: string(topic.TurnsState), Health: string(topic.Health),
		CreatedAt: topic.CreatedAt, LastActivityAt: topic.LastActivityAt,
		Pinned: topic.Pinned, SortOrder: topic.SortOrder,
		Recovered: recoveryOnly || canonicalRecovery, RecoveryState: recoveryState,
		RecoveryBranchCount: recoveryBranchCount, RecoveryUnresolvedCount: recoveryUnresolvedCount,
		RecoveryCleanupEligibleCount: recoveryCleanupEligibleCount,
		Open:                         overlay.open, Running: overlay.running, Status: overlay.status,
		// Ordinary tree is zero-config: never surface recovery counts, badges,
		// or forced-handling status. History "other saved versions" owns that.
		Children: []ProjectNode{},
	}
	// Fall back to topic-local preference when the workspace map is unavailable
	// so multi-fork topics still collapse instead of listing every replica.
	localPreferred := preferred
	if localPreferred == nil {
		localPreferred = sessioncatalog.PreferredOrdinarySessionPaths(topic.Sessions)
	}
	visible := make([]sessioncatalog.SessionRecord, 0, len(topic.Sessions))
	runtimeSessions := make([]runtimeSessionStatus, 0, len(topic.Sessions))
	for _, session := range topic.Sessions {
		sessionOverlay := sessionOverlays[sessionRuntimeKey(session.Path)]
		// Aggregate open/running state from every physical member onto the
		// single logical row — never expand recovery runtimes as children.
		if sessionOverlay.open {
			node.Open = true
		}
		if sessionOverlay.running {
			node.Running = true
			if node.Status == "" {
				node.Status = sessionOverlay.status
			}
		}
		// 1.23 ordinary-list contract: hide idle covered copies and non-
		// preferred conflict forks. Open/running recovery is still not a
		// second row — status is already aggregated above.
		if !sessioncatalog.OrdinaryTreeSession(session, false, false, localPreferred) {
			continue
		}
		visible = append(visible, session)
		runtimeSessions = append(runtimeSessions, runtimeSessionStatus{
			open: sessionOverlay.open, running: sessionOverlay.running,
		})
	}
	summary := topicSummaryFromCatalogTopic(topic, visible)
	if !recoveryOnly && topicHiddenAsRecoveryOnly(summary, topic.Pinned, append(runtimeSessions, runtimeSessionStatus{
		open: overlay.open || node.Open, running: overlay.running || node.Running,
	})) {
		return ProjectNode{Children: []ProjectNode{}}, false
	}
	if a.ordinaryTreeHidesUnindexedBlank(topic) {
		return ProjectNode{Children: []ProjectNode{}}, false
	}
	// After filtering non-preferred recovery forks, a topic may have nothing
	// left. A recovery-only topic still gets one logical row so the user can
	// reach its saved content; the physical copies remain history-only.
	if len(visible) == 0 {
		if recoveryOnly {
			representative := recoveryOnlyRepresentative(topic.Sessions)
			node.Recovered = true
			node.Turns = representative.Turns
			node.Preview = strings.TrimSpace(representative.Preview)
			node.SessionPath = representative.Path
			return node, true
		}
		if topic.Pinned || overlay.open || overlay.running || node.Open || node.Running {
			return node, true
		}
		return ProjectNode{Children: []ProjectNode{}}, false
	}
	// Ordinary list is always one logical row. Multiple normal non-recovery
	// sessions under one topic also collapse: open/running already aggregated.
	// History "other saved versions" is the only place physical forks appear.
	if live := a.liveSessionPathForTopic(topic.Scope, topic.WorkspaceRoot, topic.TopicID); live != "" {
		node.SessionPath = live
	} else if rep := strings.TrimSpace(topic.RepresentativePath); rep != "" {
		node.SessionPath = rep
	} else if path := sessioncatalog.CanonicalSessionPathForTopic(visible, ""); path != "" {
		node.SessionPath = path
	} else if len(visible) == 1 {
		node.SessionPath = visible[0].Path
	}
	return node, true
}

func (a *App) ordinaryTreeHidesUnindexedBlank(topic sessioncatalog.TopicRecord) bool {
	if topic.Pinned || topic.Turns > 0 {
		return false
	}
	if !isDefaultTopicTitle(topic.Title) && strings.TrimSpace(topic.Title) != "" {
		return false
	}
	for _, session := range topic.Sessions {
		if session.Turns > 0 || strings.TrimSpace(session.Preview) != "" {
			return false
		}
	}
	return !topicIndexedInRegistry(topic.Scope, topic.WorkspaceRoot, topic.TopicID)
}

func topicSummaryFromCatalogTopic(topic sessioncatalog.TopicRecord, visible []sessioncatalog.SessionRecord) topicSummary {
	summary := topicSummary{turns: topic.Turns, lastActivityAt: topic.LastActivityAt}
	if len(visible) == 0 {
		// Catalog still has only covered recovery copies for this topic.
		if topic.RecoveryState == "recovery_only" {
			summary.hasRecoveryOnly = true
		}
		return summary
	}
	for _, session := range visible {
		if session.RecoveryCopy {
			summary.hasRecoveryOnly = true
			continue
		}
		if session.Recovered || strings.TrimSpace(session.RecoveryDigest) != "" {
			summary.hasAdoptedRecovery = true
			if session.Turns > summary.adoptedRecoveryTurns {
				summary.adoptedRecoveryTurns = session.Turns
			}
			continue
		}
		summary.hasNormalSession = true
	}
	if topic.RecoveryState == "recovery_only" && !summary.hasNormalSession && !summary.hasAdoptedRecovery {
		summary.hasRecoveryOnly = true
	}
	return summary
}

func (a *App) listProjectTopics(req ProjectTopicPageRequest) (ProjectTopicPage, error) {
	catalog := a.sessionCatalog.Load()
	if catalog == nil {
		return a.metadataTopicPage(req)
	}
	var availability catalogWorkspaceAvailability
	if req.readAvailability != nil {
		availability = *req.readAvailability
	} else {
		availDone := debugTickPhase("list-topics:availability:" + req.WorkspaceRoot)
		availability = a.catalogWorkspaceAvailability(catalog, req.Scope, req.WorkspaceRoot, req.readContext)
		availDone()
	}
	if !availability.usable {
		// A freshly opened catalog cache is live but empty until the first directory
		// scan. Treat that the same as "catalog unavailable" so upgrade does
		// not blank the sidebar that desktop-projects.json still knows about.
		page, err := a.metadataTopicPage(req)
		if err != nil {
			return page, err
		}
		page = availability.decorate(page, catalog.Status().Revision)
		return a.withLiveTopics(catalog, req, page), nil
	}
	pageDone := debugTickPhase("list-topics:catalog-page:" + req.WorkspaceRoot)
	page, err := a.catalogTopicPage(catalog, req)
	pageDone()
	if err != nil {
		return page, err
	}
	// Metadata is a continuity source while some directories are pending or
	// degraded. Once every target has completed, the catalog is authoritative:
	// retaining metadata-only shells would resurrect recovery copies or deleted
	// sessions that the completed scan deliberately folded/removed.
	if !availability.complete {
		mergeDone := debugTickPhase("list-topics:merge-metadata:" + req.WorkspaceRoot)
		page, err = a.mergeMetadataTopics(req, page)
		mergeDone()
		if err != nil {
			return page, err
		}
	}
	page = availability.decorate(page, max(page.Revision, catalog.Status().Revision))
	liveDone := debugTickPhase("list-topics:live:" + req.WorkspaceRoot)
	defer liveDone()
	return a.withLiveTopics(catalog, req, page), nil
}

func normalizeDesktopTopicScope(scope, workspaceRoot string) (string, string) {
	if strings.TrimSpace(scope) != "project" {
		return "global", ""
	}
	return "project", strings.TrimSpace(workspaceRoot)
}

// withLiveTopics restores topics the catalog does not (yet) carry. A tab is
// authoritative for its own existence, while the catalog is a projection that
// can lag a fresh session, fall behind a stalled writer, or run degraded — and
// the sidebar must never hide a conversation this app is running. Only an
// uncursored page merges, so keyset pagination past it stays the catalog's.
func (a *App) withLiveTopics(catalog *sessioncatalog.Catalog, req ProjectTopicPageRequest, page ProjectTopicPage) ProjectTopicPage {
	if strings.TrimSpace(req.Cursor) != "" {
		return page
	}
	indexed := make(map[string]bool, len(page.Items))
	projectedPaths := make(map[string]bool, len(page.Items))
	for _, item := range page.Items {
		indexed[item.TopicID] = true
		if path := strings.TrimSpace(item.SessionPath); path != "" {
			projectedPaths[sessionRuntimeKey(path)] = true
		}
	}
	query := strings.ToLower(strings.TrimSpace(req.Query))
	live := []ProjectNode{}
	runtimeNodes, sessionsByTopic := a.runtimeOnlyProjectTopicsWithSessions(req.Scope, req.WorkspaceRoot)
	ctx, cancel := a.catalogReadContext()
	defer cancel()
	if req.readContext != nil {
		ctx = req.readContext
	}
	for _, node := range runtimeNodes {
		if indexed[node.TopicID] {
			continue
		}
		if !projectTopicRequestAllows(req, node.TopicID, node.Pinned) || !projectNodeRequestAllows(req, node) {
			continue
		}
		// A restored tab may still carry a legacy topic ID for a recovery
		// session the catalog re-anchored onto the root logical topic. That
		// logical row already represents the conversation, so a second
		// runtime-only row would break the one-row ordinary-list contract.
		if liveTopicProjectedOnPage(ctx, catalog, sessionsByTopic[node.TopicID], projectedPaths, indexed) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(node.Label), query) {
			continue
		}
		live = append(live, node)
	}
	if len(live) == 0 {
		return page
	}
	f := loadProjectsFile()
	deleted := map[string]bool{}
	for _, topicID := range f.DeletedTopics {
		deleted[topicID] = true
	}
	created := loadTopicCreatedAts(topicTitleRoot(req.Scope, req.WorkspaceRoot))
	kept := page.Items[:0:0]
	for _, node := range live {
		if deleted[node.TopicID] {
			continue
		}
		node.RuntimeOnly = true
		node.CreatedAt = topicCreatedAtForTree(created, node.TopicID)
		node.LastActivityAt = node.CreatedAt
		kept = append(kept, node)
	}
	page.Items = append(kept, page.Items...)
	return page
}

// liveTopicProjectedOnPage reports whether every catalog-known session of a
// runtime-only topic already projects onto a topic on this page. Any session
// the catalog has not indexed yet keeps the live row (that is the lag case
// withLiveTopics exists for), and an off-page projection also keeps it so an
// open conversation never disappears from the first page.
func liveTopicProjectedOnPage(ctx context.Context, catalog *sessioncatalog.Catalog, paths []string, projectedPaths, projectedTopicIDs map[string]bool) bool {
	if catalog == nil || len(paths) == 0 {
		return false
	}
	for _, path := range paths {
		record, ok, err := catalog.GetSession(ctx, path)
		if err != nil || !ok {
			return false
		}
		logicalTopicID := strings.TrimSpace(record.LogicalTopicID)
		if logicalTopicID == "" {
			logicalTopicID = strings.TrimSpace(record.TopicID)
		}
		if !projectedPaths[sessionRuntimeKey(path)] && !projectedTopicIDs[logicalTopicID] {
			return false
		}
	}
	return true
}

func (a *App) catalogTopicPage(catalog *sessioncatalog.Catalog, req ProjectTopicPageRequest) (ProjectTopicPage, error) {
	if !req.readAllSources && manualSessionOrderFor(req.Scope, req.WorkspaceRoot) {
		return a.catalogSessionOrderedPage(catalog, req)
	}
	out := ProjectTopicPage{Items: []ProjectNode{}}
	manualOrder := manualTopicOrderFor(req.Scope, req.WorkspaceRoot)
	if req.readAllSources {
		manualOrder = false
	}
	limit := req.Limit
	if limit <= 0 {
		limit = sessioncatalog.DefaultLimit
	}
	if limit > sessioncatalog.MaxLimit {
		limit = sessioncatalog.MaxLimit
	}
	topicOverlays, sessionOverlays := a.catalogRuntimeOverlays()
	ctx, cancel := a.catalogReadContext()
	defer cancel()
	if req.readContext != nil {
		ctx = req.readContext
	}
	// Workspace-wide preference collapses cross-topic recovery replicas that
	// share a lineage but were indexed as separate topic rows.
	prefDone := debugTickPhase("catalog-page:preferred:" + req.WorkspaceRoot)
	preferred, prefErr := catalog.PreferredOrdinarySessionPaths(ctx, req.Scope, req.WorkspaceRoot)
	prefDone()
	if prefErr != nil {
		preferred = nil
	}
	cursor := req.Cursor
	// Keep scanning past pages that are entirely idle recovery copies so the
	// sidebar never shows an empty "no sessions" state when later pages still
	// have ordinary topics.
	for {
		listDone := debugTickPhase("catalog-page:list-topics:" + req.WorkspaceRoot)
		page, err := catalog.ListTopics(ctx, sessioncatalog.TopicPageRequest{
			Scope: req.Scope, WorkspaceRoot: req.WorkspaceRoot, Cursor: cursor,
			Limit: limit, Query: req.Query, TimeFilter: req.TimeFilter, SortMode: req.SortMode,
			ManualOrder: manualOrder, IncludeTopicIDsJSON: req.groupIncludeJSON,
			ExcludeTopicIDsJSON: req.groupExcludeJSON, ExcludePinned: req.ExcludePinned,
			PinnedOnly:    req.pinnedOnly,
			CursorBinding: req.groupCursorBind,
		})
		listDone()
		if err != nil {
			return out, err
		}
		out.Revision = page.Revision
		for i, topic := range page.Items {
			nodes := a.projectNodesFromCatalogTopic(topic, topicOverlays, sessionOverlays, preferred)
			filtered := nodes[:0]
			for _, node := range nodes {
				if projectNodeRequestAllows(req, node) {
					filtered = append(filtered, node)
				}
			}
			nodes = filtered
			if len(nodes) == 0 {
				continue
			}
			out.Items = append(out.Items, nodes...)
			// Keep every session belonging to one historical topic on the same
			// page. The page may exceed limit by that topic's expansion, and the
			// cursor advances past the whole topic so no sibling is skipped.
			if len(out.Items) >= limit {
				if i+1 < len(page.Items) || page.NextCursor != "" {
					out.NextCursor = encodeProjectTopicCursor(topic, req.SortMode, manualOrder, req.groupCursorBind)
				}
				return out, nil
			}
		}
		if page.NextCursor == "" {
			out.NextCursor = ""
			return out, nil
		}
		cursor = page.NextCursor
	}
}

func projectTopicSortValue(createdAt, lastActivityAt int64, sortMode string) int64 {
	if strings.TrimSpace(sortMode) == "created" {
		if createdAt > 0 {
			return createdAt
		}
		return lastActivityAt
	}
	if lastActivityAt > 0 {
		return lastActivityAt
	}
	return createdAt
}

func (a *App) GetSessionCatalogStatus() SessionCatalogStatus {
	return a.currentSessionCatalogStatus()
}

// ListProjectTree is the one-release compatibility wrapper. It composes only
// catalog pages and project shells; it never migrates, scans, or decodes a
// session synchronously.
func (a *App) ListProjectTree() ([]ProjectNode, error) {
	snapshot, err := a.GetProjectTreeSnapshot()
	if err != nil {
		return []ProjectNode{}, err
	}
	hasGlobal := false
	for _, project := range snapshot.Projects {
		if project.Kind == "global_folder" {
			hasGlobal = true
			break
		}
	}
	if !hasGlobal && len(a.metadataProjectTopics("global", "")) > 0 {
		f := loadProjectsFile()
		label := strings.TrimSpace(f.GlobalTitle)
		if label == "" {
			label = "Global"
		}
		snapshot.Projects = append(snapshot.Projects, ProjectNode{
			Key: "global_folder", Kind: "global_folder", Label: label,
			Root: globalWorkspaceRoot(), ProjectColor: normalizeProjectColor(f.GlobalColor), Children: []ProjectNode{},
		})
		snapshot.Projects = applyPinnedProjectOrder(applyProjectTreeOrder(snapshot.Projects, f.SidebarOrder), f.PinnedProjects)
	}
	for index := range snapshot.Projects {
		project := &snapshot.Projects[index]
		// The lightweight snapshot carries pinned topic shells for collapsed
		// folders. This compatibility wrapper rebuilds the complete child list,
		// so start clean to avoid duplicating those shells with catalog rows.
		project.Children = []ProjectNode{}
		if project.Remote != nil {
			continue
		}
		scope := "project"
		root := project.Root
		if project.Kind == "global_folder" {
			scope = "global"
			root = ""
		}
		cursor := ""
		for {
			page, err := a.ListProjectTopics(ProjectTopicPageRequest{Scope: scope, WorkspaceRoot: root, Cursor: cursor, Limit: sessioncatalog.MaxLimit})
			if err != nil {
				return nil, err
			}
			if cursor == "" {
				defer a.ReleaseReadSnapshot(page.SnapshotID)
			}
			project.Children = append(project.Children, page.Items...)
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
	}
	return snapshot.Projects, nil
}

func (a *App) catalogSessionPathForTopic(scope, workspaceRoot, topicID string) string {
	if strings.TrimSpace(topicID) == "" {
		return ""
	}
	catalog := a.sessionCatalog.Load()
	if catalog == nil {
		return ""
	}
	topic, ok, err := catalog.GetTopic(a.bootContext(), sessioncatalog.TopicKey{Scope: scope, WorkspaceRoot: workspaceRoot, TopicID: topicID})
	if err != nil || !ok || len(topic.Sessions) == 0 {
		return ""
	}
	if representative := strings.TrimSpace(topic.RepresentativePath); representative != "" {
		return representative
	}
	if canonical := sessioncatalog.CanonicalSessionPathForTopic(topic.Sessions, ""); canonical != "" {
		return canonical
	}
	preferred := sessioncatalog.PreferredOrdinarySessionPaths(topic.Sessions)
	sort.SliceStable(topic.Sessions, func(i, j int) bool {
		// Prefer ordinary-tree survivors, then real conversations over copies.
		iPref := sessioncatalog.OrdinaryTreeSession(topic.Sessions[i], false, false, preferred)
		jPref := sessioncatalog.OrdinaryTreeSession(topic.Sessions[j], false, false, preferred)
		if iPref != jPref {
			return iPref
		}
		if topic.Sessions[i].RecoveryCopy != topic.Sessions[j].RecoveryCopy {
			return !topic.Sessions[i].RecoveryCopy
		}
		return topic.Sessions[i].LastActivityAt > topic.Sessions[j].LastActivityAt
	})
	return topic.Sessions[0].Path
}
