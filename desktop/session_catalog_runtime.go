package main

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/sessioncatalog"
)

type catalogRuntimeSnapshot struct {
	scope         string
	workspaceRoot string
	topicID       string
	sessionPath   string
	activity      string
	topicTitle    string
	ctrl          control.SessionAPI
	open          bool
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
		if status == "" || status == topicStatusError || status == topicStatusPaused {
			return topicStatusThinking
		}
		return status
	}
	if runtimeStatus.BackgroundJobs > 0 {
		return topicStatusBackgroundJob
	}
	if status == topicStatusError || status == topicStatusPaused {
		return status
	}
	return status
}

func (a *App) catalogRuntimeOverlays() (map[string]catalogRuntimeOverlay, map[string]catalogRuntimeOverlay) {
	a.mu.RLock()
	snapshots := make([]catalogRuntimeSnapshot, 0, len(a.tabs)+len(a.detachedSessions))
	collect := func(tab *WorkspaceTab, open bool) {
		if tab == nil || strings.TrimSpace(tab.TopicID) == "" {
			return
		}
		snapshots = append(snapshots, catalogRuntimeSnapshot{
			scope: tab.Scope, workspaceRoot: tab.WorkspaceRoot, topicID: tab.TopicID,
			sessionPath: tab.SessionPath, activity: tab.ActivityStatus, topicTitle: tab.TopicTitle,
			ctrl: tab.Ctrl, open: open,
		})
	}
	for _, tab := range a.tabs {
		collect(tab, true)
	}
	for _, tab := range a.detachedSessions {
		collect(tab, false)
	}
	a.mu.RUnlock()
	topics := map[string]catalogRuntimeOverlay{}
	sessions := map[string]catalogRuntimeOverlay{}
	for _, snap := range snapshots {
		runtimeStatus := control.RuntimeStatus{}
		path := strings.TrimSpace(snap.sessionPath)
		if snap.ctrl != nil {
			runtimeStatus = snap.ctrl.RuntimeStatus()
			if path == "" {
				path = snap.ctrl.SessionPath()
			}
		}
		status := catalogRuntimeStatus(snap.activity, runtimeStatus)
		running := status != "" || runtimeStatus.Running || runtimeStatus.PendingPrompt || runtimeStatus.BackgroundJobs > 0
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
	ids := f.GlobalTopics
	pinnedIDs := f.GlobalPinnedTopics
	titleRoot := ""
	projectColor := normalizeProjectColor(f.GlobalColor)
	if scope == "project" {
		ids = nil
		pinnedIDs = nil
		titleRoot = workspaceRoot
		for _, project := range f.Projects {
			if sameProjectRoot(project.Root, workspaceRoot) {
				ids = project.Topics
				pinnedIDs = project.PinnedTopics
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
	runtimeByTopic := map[string]ProjectNode{}
	for _, node := range runtimeNodes {
		runtimeByTopic[node.TopicID] = node
	}
	out := []ProjectNode{}
	seen := map[string]bool{}
	for _, topicID := range pinnedTopicIDs(orderedTopicIDs(ids, titles), pinnedIDs) {
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
			CreatedAt: topicCreatedAtForTree(created, topicID), Pinned: containsDesktopString(pinnedIDs, topicID),
			Open: overlay.open, Running: overlay.running, Status: overlay.status,
			TurnsState: string(sessioncatalog.TurnsUnknown), Health: string(sessioncatalog.HealthOK),
			Children: []ProjectNode{},
		}
		if runtimeNode, ok := runtimeByTopic[topicID]; ok {
			node.Open = runtimeNode.Open
			node.Running = runtimeNode.Running
			node.Status = runtimeNode.Status
			node.Children = runtimeNode.Children
		}
		out = append(out, node)
	}
	for _, runtimeNode := range runtimeNodes {
		if seen[runtimeNode.TopicID] || deleted[runtimeNode.TopicID] {
			continue
		}
		out = append(out, runtimeNode)
	}
	return out
}

func (a *App) runtimeOnlyProjectTopics(scope, workspaceRoot string) []ProjectNode {
	a.mu.RLock()
	snapshots := []catalogRuntimeSnapshot{}
	collect := func(tab *WorkspaceTab, open bool) {
		if tab == nil || strings.TrimSpace(tab.TopicID) == "" {
			return
		}
		if scope == "project" {
			if tab.Scope != "project" || !sameProjectRoot(tab.WorkspaceRoot, workspaceRoot) {
				return
			}
		} else if tab.Scope == "project" {
			return
		}
		snapshots = append(snapshots, catalogRuntimeSnapshot{
			scope: tab.Scope, workspaceRoot: tab.WorkspaceRoot, topicID: tab.TopicID,
			sessionPath: tab.SessionPath, activity: tab.ActivityStatus,
			topicTitle: tab.TopicTitle, ctrl: tab.Ctrl, open: open,
		})
	}
	for _, tab := range a.tabs {
		collect(tab, true)
	}
	for _, tab := range a.detachedSessions {
		collect(tab, false)
	}
	a.mu.RUnlock()
	byTopic := map[string][]catalogRuntimeSnapshot{}
	for _, snapshot := range snapshots {
		if snapshot.sessionPath == "" && snapshot.ctrl != nil {
			snapshot.sessionPath = snapshot.ctrl.SessionPath()
		}
		byTopic[snapshot.topicID] = append(byTopic[snapshot.topicID], snapshot)
	}
	topicIDs := make([]string, 0, len(byTopic))
	for topicID := range byTopic {
		topicIDs = append(topicIDs, topicID)
	}
	sort.Strings(topicIDs)
	out := []ProjectNode{}
	for _, topicID := range topicIDs {
		sessions := byTopic[topicID]
		kind := "topic"
		sessionKind := "session"
		if scope != "project" {
			kind = "global_topic"
			sessionKind = "global_session"
		}
		label := defaultTopicTitle
		if strings.TrimSpace(sessions[0].topicTitle) != "" {
			label = sessions[0].topicTitle
		}
		node := ProjectNode{
			Key: kind + "_" + topicID, Kind: kind, Label: label,
			Root: workspaceRoot, TopicID: topicID, TurnsState: string(sessioncatalog.TurnsUnknown),
			Health: string(sessioncatalog.HealthOK), Children: []ProjectNode{},
		}
		for _, session := range sessions {
			runtimeStatus := control.RuntimeStatus{}
			if session.ctrl != nil {
				runtimeStatus = session.ctrl.RuntimeStatus()
			}
			status := catalogRuntimeStatus(session.activity, runtimeStatus)
			running := status != "" || runtimeStatus.Running || runtimeStatus.PendingPrompt || runtimeStatus.BackgroundJobs > 0
			if len(sessions) == 1 {
				node.Open = session.open
				node.Running = running
				node.Status = status
				continue
			}
			path := strings.TrimSpace(session.sessionPath)
			sessionLabel := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			if sessionLabel == "" || sessionLabel == "." {
				sessionLabel = label
			}
			node.Children = append(node.Children, ProjectNode{
				Key: projectSessionNodeKey(scope, path), Kind: sessionKind, Label: sessionLabel,
				Root: workspaceRoot, TopicID: topicID, SessionPath: path,
				Open: session.open, Running: running, Status: status,
				TurnsState: string(sessioncatalog.TurnsUnknown), Health: string(sessioncatalog.HealthOK),
				Children: []ProjectNode{},
			})
		}
		out = append(out, node)
	}
	return out
}

func (a *App) metadataTopicPage(req ProjectTopicPageRequest) ProjectTopicPage {
	items := a.metadataProjectTopics(req.Scope, req.WorkspaceRoot)
	query := strings.ToLower(strings.TrimSpace(req.Query))
	if query != "" {
		filtered := items[:0]
		for _, item := range items {
			if strings.Contains(strings.ToLower(item.Label), query) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	start := 0
	if lastID, ok := strings.CutPrefix(req.Cursor, "meta:"); ok {
		for index, item := range items {
			if item.TopicID == lastID {
				start = index + 1
				break
			}
		}
	}
	limit := req.Limit
	if limit <= 0 {
		limit = sessioncatalog.DefaultLimit
	}
	if limit > sessioncatalog.MaxLimit {
		limit = sessioncatalog.MaxLimit
	}
	end := min(start+limit, len(items))
	page := ProjectTopicPage{Items: append([]ProjectNode(nil), items[start:end]...)}
	if end < len(items) && end > start {
		page.NextCursor = "meta:" + items[end-1].TopicID
	}
	return page
}

func (a *App) projectNodeFromCatalogTopic(topic sessioncatalog.TopicRecord, topicOverlays, sessionOverlays map[string]catalogRuntimeOverlay) (ProjectNode, bool) {
	kind := "topic"
	if topic.Scope == "global" {
		kind = "global_topic"
	}
	overlay := topicOverlays[topicSummaryKey(topic.Scope, topic.WorkspaceRoot, topic.TopicID)]
	node := ProjectNode{
		Key: kind + "_" + topic.TopicID, Kind: kind, Label: a.localizedTopicTitle(topic.Title, ""),
		Root: topic.WorkspaceRoot, TopicID: topic.TopicID, Turns: topic.Turns,
		TurnsState: string(topic.TurnsState), Health: string(topic.Health),
		CreatedAt: topic.CreatedAt, LastActivityAt: topic.LastActivityAt,
		Pinned: topic.Pinned, Open: overlay.open, Running: overlay.running, Status: overlay.status,
		Children: []ProjectNode{},
	}
	visible := make([]sessioncatalog.SessionRecord, 0, len(topic.Sessions))
	runtimeSessions := make([]runtimeSessionStatus, 0, len(topic.Sessions))
	for _, session := range topic.Sessions {
		sessionOverlay := sessionOverlays[sessionRuntimeKey(session.Path)]
		// Idle covered recovery copies stay out of the ordinary tree. Open or
		// running copies remain reachable so the user can still inspect them.
		if session.RecoveryCopy && !sessionOverlay.open && !sessionOverlay.running {
			continue
		}
		visible = append(visible, session)
		runtimeSessions = append(runtimeSessions, runtimeSessionStatus{
			open: sessionOverlay.open, running: sessionOverlay.running,
		})
	}
	summary := topicSummaryFromCatalogTopic(topic, visible)
	if topicHiddenAsRecoveryOnly(summary, topic.Pinned, append(runtimeSessions, runtimeSessionStatus{
		open: overlay.open, running: overlay.running,
	})) {
		return ProjectNode{Children: []ProjectNode{}}, false
	}
	// A single effective session collapses to a normal topic row.
	if len(visible) <= 1 {
		return node, true
	}
	for _, session := range visible {
		sessionKind := "session"
		if topic.Scope == "global" {
			sessionKind = "global_session"
		}
		sessionOverlay := sessionOverlays[sessionRuntimeKey(session.Path)]
		label := strings.TrimSpace(session.CustomTitle)
		if label == "" {
			label = strings.TrimSpace(session.Preview)
		}
		if label == "" {
			label = filepath.Base(session.Path)
		}
		node.Children = append(node.Children, ProjectNode{
			Key: projectSessionNodeKey(topic.Scope, session.Path), Kind: sessionKind,
			Label: label, Root: topic.WorkspaceRoot, TopicID: topic.TopicID,
			SessionPath: session.Path, Turns: session.Turns,
			TurnsState: string(session.TurnsState), Health: string(session.Health),
			CreatedAt: session.CreatedAt, LastActivityAt: session.LastActivityAt,
			Open: sessionOverlay.open, Running: sessionOverlay.running, Status: sessionOverlay.status,
			Recovered: session.Recovered, RecoveryReason: session.RecoveryReason,
			RecoveryDigest: session.RecoveryDigest, RecoveryParentID: session.ParentID,
			Children: []ProjectNode{},
		})
	}
	return node, true
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

func (a *App) ListProjectTopics(req ProjectTopicPageRequest) (ProjectTopicPage, error) {
	out := ProjectTopicPage{Items: []ProjectNode{}}
	catalog := a.sessionCatalog.Load()
	if catalog == nil {
		return a.metadataTopicPage(req), nil
	}
	limit := req.Limit
	if limit <= 0 {
		limit = sessioncatalog.DefaultLimit
	}
	if limit > sessioncatalog.MaxLimit {
		limit = sessioncatalog.MaxLimit
	}
	topicOverlays, sessionOverlays := a.catalogRuntimeOverlays()
	cursor := req.Cursor
	// Keep scanning past pages that are entirely idle recovery copies so the
	// sidebar never shows an empty "no sessions" state when later pages still
	// have ordinary topics.
	for {
		page, err := catalog.ListTopics(a.bootContext(), sessioncatalog.TopicPageRequest{
			Scope: req.Scope, WorkspaceRoot: req.WorkspaceRoot, Cursor: cursor,
			Limit: limit, Query: req.Query, TimeFilter: req.TimeFilter,
		})
		if err != nil {
			return out, err
		}
		out.Revision = page.Revision
		for i, topic := range page.Items {
			node, ok := a.projectNodeFromCatalogTopic(topic, topicOverlays, sessionOverlays)
			if !ok {
				continue
			}
			out.Items = append(out.Items, node)
			if len(out.Items) == limit {
				if i+1 < len(page.Items) || page.NextCursor != "" {
					out.NextCursor = encodeProjectTopicCursor(topic)
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

func encodeProjectTopicCursor(topic sessioncatalog.TopicRecord) string {
	// Reuse the catalog's keyset cursor encoding by asking for the next page
	// after this topic. ListTopics accepts the same opaque cursor it emits.
	pinned := 0
	if topic.Pinned {
		pinned = 1
	}
	return sessioncatalog.EncodeTopicCursor(pinned, topic.LastActivityAt, topic.TopicID)
}

func (a *App) GetTopicSummary(key ProjectTopicKey) (ProjectNode, error) {
	if catalog := a.sessionCatalog.Load(); catalog != nil {
		topic, ok, err := catalog.GetTopic(a.bootContext(), sessioncatalog.TopicKey{
			Scope: key.Scope, WorkspaceRoot: key.WorkspaceRoot, TopicID: key.TopicID,
		})
		if err != nil {
			return ProjectNode{Children: []ProjectNode{}}, err
		}
		if ok {
			topicOverlays, sessionOverlays := a.catalogRuntimeOverlays()
			if node, visible := a.projectNodeFromCatalogTopic(topic, topicOverlays, sessionOverlays); visible {
				return node, nil
			}
			return ProjectNode{Children: []ProjectNode{}}, nil
		}
	}
	page, err := a.ListProjectTopics(ProjectTopicPageRequest{
		Scope: key.Scope, WorkspaceRoot: key.WorkspaceRoot, Limit: sessioncatalog.MaxLimit,
	})
	if err != nil {
		return ProjectNode{Children: []ProjectNode{}}, err
	}
	for _, node := range page.Items {
		if node.TopicID == key.TopicID {
			return node, nil
		}
	}
	return ProjectNode{Children: []ProjectNode{}}, nil
}

func (a *App) GetSessionCatalogStatus() SessionCatalogStatus {
	return a.currentSessionCatalogStatus()
}

func (a *App) RebuildSessionCatalog() error {
	if a == nil || a.shuttingDown.Load() {
		return errors.New("application is shutting down")
	}
	if !a.catalogRebuilding.CompareAndSwap(false, true) {
		return nil
	}
	go func() {
		a.stopSessionCatalog(250 * time.Millisecond)
		a.catalogRebuilding.Store(false)
		a.startSessionCatalog(true)
	}()
	return nil
}

// ListProjectTree is the one-release compatibility wrapper. It composes only
// catalog pages and project shells; it never migrates, scans, or decodes a
// session synchronously.
func (a *App) ListProjectTree() []ProjectNode {
	snapshot := a.GetProjectTreeSnapshot()
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
				break
			}
			project.Children = append(project.Children, page.Items...)
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
		if len(project.Children) == 0 {
			project.Children = a.metadataProjectTopics(scope, root)
		}
	}
	return snapshot.Projects
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
	sort.SliceStable(topic.Sessions, func(i, j int) bool {
		// Prefer real conversations over idle covered recovery copies.
		if topic.Sessions[i].RecoveryCopy != topic.Sessions[j].RecoveryCopy {
			return !topic.Sessions[i].RecoveryCopy
		}
		return topic.Sessions[i].LastActivityAt > topic.Sessions[j].LastActivityAt
	})
	return topic.Sessions[0].Path
}
