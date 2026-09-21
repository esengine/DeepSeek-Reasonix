package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"reasonix/desktop/internal/workspacestate"
	"reasonix/internal/session"
)

// Both recovery and the sidebar resolve durable registry members. Legacy
// catalog rows remain available only until their exact source is adopted.
func (a *App) unifiedProjectTopics(req ProjectTopicPageRequest) (ProjectTopicPage, error) {
	scope, root, err := normalizeOrganizationTarget(req.Scope, req.WorkspaceRoot)
	if err != nil {
		return ProjectTopicPage{Items: []ProjectNode{}}, err
	}
	req.Scope, req.WorkspaceRoot = scope, root
	workspaceID, org, err := a.ensureSessionOrganization(scope, root)
	if err != nil {
		return ProjectTopicPage{Items: []ProjectNode{}}, err
	}
	state, err := a.workspaceRegistry().LoadProjection(a.bootContext())
	if err != nil {
		return ProjectTopicPage{Items: []ProjectNode{}}, err
	}
	return a.projectTopicsFromProjection(req, state, workspacestate.NewWorkspaceIndex(state), workspaceID, org, nil)
}

// projectTopicsFromProjection materializes one workspace from a caller-owned
// registry snapshot. Project-tree reads use it without organization migration
// or repeated registry loads, so building sidebar shells remains read-only.
func (a *App) projectTopicsFromProjection(req ProjectTopicPageRequest, state workspacestate.State, workspaceIndex *workspacestate.WorkspaceIndex, workspaceID string, org workspacestate.Organization, shellPreferences *desktopProject) (ProjectTopicPage, error) {
	catalogRevision := a.currentSessionCatalogStatus().Revision
	workspace := state.Workspaces[workspaceID]
	reader := a.desktopSessionService("").Query()
	infos, _ := listWorkspaceSessionInfo(a.bootContext(), reader, workspace.SessionIDs)
	groups := organizationSnapshot(org, true).Groups
	req.groupInclude = nil
	req.groupExclude = nil
	req.groupIncludeJSON = ""
	req.groupExcludeJSON = ""
	req.groupAll = groups
	req.groupSelected = nil
	if req.GroupFilter == "group" {
		for i := range groups {
			if groups[i].ID == req.GroupID {
				req.groupSelected = &groups[i]
				break
			}
		}
		if req.groupSelected == nil {
			return ProjectTopicPage{Items: []ProjectNode{}}, fmt.Errorf("session group no longer exists")
		}
	}
	adopted := map[string]bool{}
	adoptedTopics := map[string]bool{}
	for _, m := range state.SourceMappings {
		if m.WorkspaceID == workspaceID {
			adopted["source\x00local\x00"+m.SourceKey] = true
			if sourceMappingHasPathAlias(m) {
				adopted[sessionRuntimeKey(m.Path)] = true
			}
		}
	}
	for _, id := range workspace.SessionIDs {
		adoptedTopics[state.Presentation[id].TopicID] = true
	}
	all := req
	all.Cursor = ""
	all.Query = ""
	all.TimeFilter = ""
	all.GroupFilter = "all"
	all.ExcludePinned = false
	all.groupSelected = nil
	all.groupAll = nil
	legacy, err := a.unadoptedLegacyTopics(all, adopted, adoptedTopics)
	if err != nil {
		return legacy, err
	}
	sources := append(legacy.Items, a.historicalCanonicalTopicsFromProjection(req.Scope, req.WorkspaceRoot, state, workspaceIndex)...)
	if saved, err := readHistoricalSidecar(); err == nil {
		applyHistoricalPresentations(sources, saved)
	}
	if shellPreferences != nil {
		org = projectedShellOrganization(workspace, state, sources, *shellPreferences)
	}
	filtered := a.indexedWorkspaceTopics(req, state, workspace, org, infos, sources)
	// Bind to the exact materialized order and metadata, plus owner revisions.
	// Runtime decoration is deliberately excluded: opening a tab is not a reorder.
	identity := []any{state.Generation, org.Revision, legacy.Revision, projectTopicCursorBinding(req, req.GroupFilter, req.GroupID, org.Revision)}
	for _, n := range filtered {
		identity = append(identity, []any{projectNodeSessionKey(n), n.Label, n.Preview, n.Pinned, n.CreatedAt, n.LastActivityAt, n.ResultSequence, n.SortOrder, n.LifecycleGeneration})
	}
	encoded, _ := json.Marshal(identity)
	digest := sha256.Sum256(encoded)
	prefix := fmt.Sprintf("sessions:%x:", digest[:])
	offset := 0
	if req.Cursor != "" {
		if !strings.HasPrefix(req.Cursor, prefix) {
			return ProjectTopicPage{Items: []ProjectNode{}}, newSessionOperationError("stale_cursor", "The session list changed. Reload it.")
		}
		offset, err = strconv.Atoi(strings.TrimPrefix(req.Cursor, prefix))
		if err != nil || offset < 0 || offset > len(filtered) {
			return ProjectTopicPage{Items: []ProjectNode{}}, newSessionOperationError("stale_cursor", "The session list changed. Reload it.")
		}
	}
	if shellPreferences == nil {
		after, err := a.workspaceRegistry().LoadProjection(a.bootContext())
		if err != nil {
			return ProjectTopicPage{Items: []ProjectNode{}}, err
		}
		if after.Generation != state.Generation || a.currentSessionCatalogStatus().Revision != catalogRevision || !workspaceSessionInfoUnchanged(a.bootContext(), reader, workspace.SessionIDs, infos) {
			return ProjectTopicPage{Items: []ProjectNode{}}, newSessionOperationError("stale_cursor", "The session list changed. Reload it.")
		}
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 200)
	end := min(offset+limit, len(filtered))
	legacy.Items = cloneTopicPage(filtered[offset:end])
	for i := range legacy.Items {
		if ref := legacy.Items[i].Session; ref != nil {
			_, legacy.Items[i].Open = a.desktopSessionService("").Runtime(*ref)
		}
	}
	legacy.Revision += state.Generation
	legacy.NextCursor = ""
	if end < len(filtered) {
		legacy.NextCursor = prefix + strconv.Itoa(end)
	}
	return legacy, nil
}

func desktopSessionTimeCutoff(filter string) int64 {
	value := strings.ToLower(strings.TrimSpace(filter))
	switch value {
	case "day":
		value = "24h"
	case "week", "7d":
		value = "168h"
	case "month", "30d":
		value = "720h"
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0
	}
	return time.Now().Add(-duration).UnixMilli()
}

func (a *App) unifiedProjectRevision(catalogRevision uint64) uint64 {
	state, err := a.workspaceRegistry().LoadProjection(a.bootContext())
	if err != nil {
		return catalogRevision
	}
	return catalogRevision + state.Generation
}

func (a *App) updateCanonicalTopicPresentation(topicID string, title *string, pinned *bool) (bool, error) {
	state, err := a.workspaceRegistry().LoadProjection(a.bootContext())
	if err != nil {
		return false, err
	}
	ids := []string{}
	for _, workspace := range state.Workspaces {
		for _, id := range workspace.SessionIDs {
			if state.Presentation[id].TopicID == topicID || "canonical-"+id == topicID {
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return false, nil
	}
	if title != nil {
		for _, id := range ids {
			ref := session.SessionRef{HostID: localDesktopHostID, SessionID: id}
			if err := a.desktopSessionService("").SetTitle(a.bootContext(), ref, *title); err != nil {
				return true, err
			}
			a.publishCanonicalSessionTitle(ref, *title)
		}
	}
	if pinned != nil {
		if err := a.workspaceRegistry().UpdatePresentation(a.bootContext(), ids, nil, pinned); err != nil {
			return true, err
		}
		a.emitProjectTreeMetadataChanged()
	}
	return true, nil
}

func (a *App) mergeCanonicalWorkspaceShells(projects []ProjectNode) []ProjectNode {
	state, err := a.workspaceRegistry().LoadProjection(a.bootContext())
	if err != nil {
		return projects
	}
	return a.mergeCanonicalWorkspaceShellsFromProjection(projects, state)
}

func (a *App) mergeCanonicalWorkspaceShellsFromProjection(projects []ProjectNode, state workspacestate.State) []ProjectNode {
	preferences := loadProjectsFile()
	workspaceIndex := workspacestate.NewWorkspaceIndex(state)
	owner := func(scope, root string) (string, bool) {
		if scope != "project" {
			_, found := state.Workspaces[workspacestate.GlobalWorkspaceID]
			return workspacestate.GlobalWorkspaceID, found
		}
		id, found, err := workspaceIndex.Resolve(root)
		return id, found && err == nil
	}
	present := map[string]bool{}
	visible := make([]ProjectNode, 0, len(projects))
	for _, project := range projects {
		if project.Remote != nil {
			visible = append(visible, project)
			continue
		}
		scope := "project"
		if project.Kind == "global_folder" {
			scope = "global"
		}
		id, found := owner(scope, project.Root)
		if workspace, ok := state.Workspaces[id]; found && ok && !workspace.Visible {
			continue
		}
		if found {
			present[id] = true
		}
		visible = append(visible, project)
	}
	projects = visible
	for _, id := range state.WorkspaceIDs {
		workspace := state.Workspaces[id]
		if !workspace.Visible || present[id] {
			continue
		}
		kind, key := "project", "project_"+workspace.Root
		if id == workspacestate.GlobalWorkspaceID {
			kind, key = "global_folder", "global_folder"
		}
		projects = append(projects, ProjectNode{Key: key, Kind: kind, Root: workspace.Root, Label: workspace.Title, Children: []ProjectNode{}})
	}
	// Pinned shells must use the same canonical rows as ordinary pages. Legacy
	// shells otherwise overwrite the title/key on refresh and resurrect pins
	// for sessions whose registry lifecycle is already archived.
	for index := range projects {
		project := &projects[index]
		if project.Remote != nil {
			continue
		}
		scope, root := "project", project.Root
		if project.Kind == "global_folder" {
			scope, root = "global", ""
		}
		req := ProjectTopicPageRequest{Scope: scope, WorkspaceRoot: root, Limit: 200, pinnedOnly: true}
		workspaceID, found := owner(scope, root)
		workspace := state.Workspaces[workspaceID]
		legacy := legacyOrganizationPreferences(preferences, scope, root)
		if !found || len(workspace.SessionIDs) == 0 {
			pins, err := a.historicalPinnedShellsFromProjection(req, state, workspaceIndex, legacy)
			if err != nil {
				project.Health = "metadata_failed"
			} else {
				project.Children = pins
			}
			continue
		}
		pins := []ProjectNode{}
		for {
			page, err := a.projectTopicsFromProjection(req, state, workspaceIndex, workspaceID, workspacestate.Organization{}, &legacy)
			if err != nil {
				project.Health = "metadata_failed"
				break
			}
			unpinned := false
			for _, node := range page.Items {
				if node.Pinned {
					pins = append(pins, node)
				} else {
					unpinned = true
				}
			}
			if unpinned || page.NextCursor == "" {
				project.Children = pins
				break
			}
			req.Cursor = page.NextCursor
		}
	}
	return projects
}

func (a *App) unadoptedLegacyTopics(req ProjectTopicPageRequest, adopted, adoptedTopics map[string]bool) (ProjectTopicPage, error) {
	legacyReq := req
	legacyReq.Cursor, legacyReq.Limit = "", 200
	legacy := ProjectTopicPage{Items: []ProjectNode{}}
	for {
		page, err := a.listProjectTopics(legacyReq)
		if err != nil {
			return page, err
		}
		legacy.Complete, legacy.ReadyDirectories, legacy.PendingDirectories, legacy.FailedDirectories = page.Complete, page.ReadyDirectories, page.PendingDirectories, page.FailedDirectories
		legacy.Revision = max(legacy.Revision, page.Revision)
		expanded := []ProjectNode{}
		for _, node := range page.Items {
			expanded = append(expanded, expandSessionSourceRows(node)...)
		}
		for _, node := range expanded {
			if node.Source != nil {
				node.PreparationStatus = a.historicalPreparationStatus(node.Source.SourceKey)
				if !adopted[projectNodeSessionKey(node)] {
					legacy.Items = append(legacy.Items, node)
				}
				continue
			}
			if adopted[sessionRuntimeKey(node.SessionPath)] || (node.SessionPath == "" && adoptedTopics[node.TopicID]) {
				remaining := []ProjectNode{}
				for _, child := range node.Children {
					if !adopted[sessionRuntimeKey(child.SessionPath)] {
						remaining = append(remaining, child)
					}
				}
				if len(remaining) > 0 {
					node.Children = remaining
					node.SessionPath = remaining[0].SessionPath
					legacy.Items = append(legacy.Items, node)
				}
				continue
			}
			legacy.Items = append(legacy.Items, node)
		}
		if page.NextCursor == "" {
			break
		}
		if page.NextCursor == legacyReq.Cursor {
			return legacy, fmt.Errorf("legacy session cursor did not advance")
		}
		legacyReq.Cursor = page.NextCursor
	}
	return legacy, nil
}

func (a *App) canonicalTopicNodes(req ProjectTopicPageRequest, state workspacestate.State, workspace workspacestate.Workspace, infos map[string]session.SessionInfo, initial []ProjectNode, created ...map[string]int64) []ProjectNode {
	workspaceID := workspace.ID
	service := a.desktopSessionService("")
	nodes := initial
	query := strings.ToLower(strings.TrimSpace(req.Query))
	cutoff := desktopSessionTimeCutoff(req.TimeFilter)
	var createdTopics map[string]int64
	if len(created) > 0 {
		createdTopics = created[0]
	} else {
		createdTopics = loadTopicCreatedAts(topicTitleRoot(req.Scope, req.WorkspaceRoot))
	}
	for index, id := range workspace.SessionIDs {
		if state.SessionStates[id].Lifecycle != workspacestate.Active {
			continue
		}
		info, found := infos[id]
		row := workspaceSessionRow(workspaceID, id, info, found, false, service)
		if found && cutoff > 0 && max(row.CreatedAt, row.UpdatedAt) < cutoff {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(row.Title+"\n"+row.Preview+"\n"+id), query) {
			continue
		}
		ref := session.SessionRef{HostID: localDesktopHostID, SessionID: id}
		presentation := state.Presentation[id]
		label := a.localizedTopicTitle(sessionDisplayTitle(info, presentation))
		kind := "topic"
		if req.Scope != "project" {
			kind = "global_topic"
		}
		topicID := presentation.TopicID
		if topicID == "" {
			topicID = "canonical-" + id
		}
		sortOrder := index
		createdAt := row.CreatedAt
		if previous := createdTopics[topicID]; previous > 0 {
			createdAt = previous
		}
		node := ProjectNode{
			Key: "canonical_" + id, Kind: kind, Label: label, Root: workspace.Root,
			TopicID: topicID, Session: &ref, SessionPath: sessionRoute(id), CanArchive: row.Health != "missing",
			Preview: row.Preview, Turns: row.Turns, TurnsState: row.MetadataStatus, Health: row.Health,
			CreatedAt: createdAt, LastActivityAt: row.UpdatedAt, ResultSequence: row.ResultSequence, Open: row.Running,
			Pinned: presentation.Pinned, SortOrder: sortOrder, Children: []ProjectNode{},
		}
		if row.ParentSessionID != "" {
			node.ParentSession = &session.SessionRef{HostID: localDesktopHostID, SessionID: row.ParentSessionID}
		}
		node.SessionOrigin = row.Origin
		if projectNodeRequestAllows(req, node) {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func filterWorkspaceSessionNodes(req ProjectTopicPageRequest, org workspacestate.Organization, state workspacestate.State, workspaceID string, nodes []ProjectNode) []ProjectNode {
	ranks := map[string]int{}
	for i, key := range org.Order {
		ranks[key] = i
	}
	filtered := []ProjectNode{}
	seen := map[string]bool{}
	cutoff := desktopSessionTimeCutoff(req.TimeFilter)
	query := strings.ToLower(strings.TrimSpace(req.Query))
	aliases := workspaceSourceAliases(state, workspaceID)
	for _, n := range nodes {
		if req.pinnedOnly && !n.Pinned {
			continue
		}
		key := projectNodeSessionKey(n)
		if seen[key] {
			continue
		}
		seen[key] = true
		n.SortOrder = -1
		if org.ManualOrderEnabled {
			if rank, ok := ranks[key]; ok {
				n.SortOrder = rank
			}
		}
		if n.Session != nil {
			n.IdentityAliases = aliases[n.Session.SessionID]
			n.LifecycleGeneration = state.SessionStates[n.Session.SessionID].Generation
		}
		if !projectNodeRequestAllows(req, n) || cutoff > 0 && max(n.CreatedAt, n.LastActivityAt) < cutoff {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(n.Label+"\n"+n.Preview+"\n"+key), query) {
			continue
		}
		filtered = append(filtered, n)
	}
	return filtered
}
