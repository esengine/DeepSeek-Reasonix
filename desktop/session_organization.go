package main

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"reasonix/desktop/internal/workspacestate"
	"reasonix/internal/session"
)

type SessionOrganizationWorkspace struct {
	Scope         string `json:"scope"`
	WorkspaceRoot string `json:"workspaceRoot,omitempty"`
	HostID        string `json:"hostId,omitempty"`
}
type SessionOrganizationSnapshot struct {
	Revision           uint64         `json:"revision"`
	Applied            bool           `json:"applied"`
	ManualOrderEnabled bool           `json:"manualOrderEnabled"`
	Order              []string       `json:"order"`
	Groups             []desktopGroup `json:"groups"`
}
type SessionOrganizationMutation struct {
	Kind     string           `json:"kind"`
	Target   *SessionSelector `json:"target,omitempty"`
	Anchor   *SessionSelector `json:"anchor,omitempty"`
	Position string           `json:"position,omitempty"`
	GroupID  string           `json:"groupId,omitempty"`
	Title    string           `json:"title,omitempty"`
}

func organizationSnapshot(o workspacestate.Organization, applied bool) SessionOrganizationSnapshot {
	groups := []desktopGroup{}
	for _, g := range o.Groups {
		groups = append(groups, desktopGroup{ID: g.ID, Title: g.Title, SessionKeys: append([]string{}, g.Members...)})
	}
	return SessionOrganizationSnapshot{Revision: o.Revision, Applied: applied, ManualOrderEnabled: o.ManualOrderEnabled, Order: append([]string{}, o.Order...), Groups: groups}
}

// Import known sources incrementally. Imported includes explicit ungrouped
// choices, so later discoveries never reinstate a topic-level preference.
func (a *App) ensureSessionOrganization(scope, root string) (string, workspacestate.Organization, error) {
	scope, root, err := normalizeOrganizationTarget(scope, root)
	if err != nil {
		return "", workspacestate.Organization{}, err
	}
	state, err := a.workspaceRegistry().LoadProjection(a.bootContext())
	if err != nil {
		return "", workspacestate.Organization{}, err
	}
	id, found := workspacestate.FindWorkspace(state, desktopWorkspaceRoot(scope, root))
	if !found {
		id, err = a.ensureDesktopWorkspace(a.bootContext(), scope, root)
		if err != nil {
			return "", workspacestate.Organization{}, err
		}
		state, err = a.workspaceRegistry().LoadProjection(a.bootContext())
		if err != nil {
			return "", workspacestate.Organization{}, err
		}
	}
	workspace := state.Workspaces[id]
	projects := loadProjectsFile()
	importKey, cacheable := a.organizationImportKey(scope, root, state, id, projects)
	if cacheable {
		if organization, ok := a.desktopSessions.organizations.get(importKey); ok {
			return id, organization, nil
		}
	}
	legacy := legacyOrganizationPreferences(projects, scope, root)
	nodes := []ProjectNode{}
	aliases := workspaceSourceAliases(state, id)
	for _, sid := range workspace.SessionIDs {
		p := state.Presentation[sid]
		ref := session.SessionRef{HostID: localDesktopHostID, SessionID: sid}
		node := ProjectNode{Session: &ref, TopicID: p.TopicID, SessionPath: sessionRoute(sid)}
		node.IdentityAliases = aliases[sid]
		nodes = append(nodes, node)
	}
	req := ProjectTopicPageRequest{Scope: scope, WorkspaceRoot: root, Limit: 200}
	for {
		page, e := a.listProjectTopics(req)
		if e != nil {
			return "", workspacestate.Organization{}, e
		}
		for _, node := range page.Items {
			nodes = append(nodes, expandSessionSourceRows(node)...)
		}
		if page.NextCursor == "" {
			break
		}
		if page.NextCursor == req.Cursor {
			return "", workspacestate.Organization{}, fmt.Errorf("legacy cursor did not advance")
		}
		req.Cursor = page.NextCursor
	}
	importSources := func(o *workspacestate.Organization) error {
		projectLegacyOrganization(o, nodes, legacy)
		return nil
	}
	// A settled import is a read. Do not enter the cross-process write lock or
	// serialize the whole registry merely to discover that nothing changed.
	if workspace.Organization != nil {
		candidate := workspace.Organization.Clone()
		if err := importSources(&candidate); err != nil {
			return "", workspacestate.Organization{}, err
		}
		if reflect.DeepEqual(candidate, *workspace.Organization) {
			if cacheable {
				a.desktopSessions.organizations.put(importKey, candidate)
			}
			return id, candidate, nil
		}
	}
	org, _, err := a.workspaceRegistry().UpdateOrganization(a.bootContext(), id, nil, importSources)
	return id, org, err
}

func legacyOrganizationPreferences(projects desktopProjectFile, scope, root string) desktopProject {
	if scope == "project" {
		// Most callers use the persisted spelling; avoid filesystem comparisons
		// unless this request actually uses an alias.
		for _, project := range projects.Projects {
			if project.Root == root {
				return project
			}
		}
		if i := projectIndexByRoot(projects.Projects, root); i >= 0 {
			return projects.Projects[i]
		}
		return desktopProject{}
	}
	return desktopProject{Groups: projects.GlobalGroups, SessionOrder: projects.GlobalSessionOrder,
		Topics: projects.GlobalTopics, ManualSessionOrder: projects.GlobalManualSessionOrder, ManualTopicOrder: projects.GlobalManualTopicOrder}
}

// projectLegacyOrganization shares the import semantics with the writer, but
// changes only the caller-owned organization. Snapshot callers supply already
// materialized nodes and never perform a second discovery pass or persist it.
func projectLegacyOrganization(o *workspacestate.Organization, nodes []ProjectNode, legacy desktopProject) {
	if o.Imported == nil {
		o.Imported = map[string]bool{}
	}
	canonicalByAlias := map[string]string{}
	for _, node := range nodes {
		if node.Session != nil {
			for _, alias := range node.IdentityAliases {
				canonicalByAlias[alias] = projectNodeSessionKey(node)
			}
		}
	}
	initial := o.MigrationVersion == 0
	manual := legacy.ManualSessionOrder || legacy.ManualTopicOrder
	if initial {
		o.ManualOrderEnabled = manual
		for _, group := range legacy.Groups {
			o.Groups = append(o.Groups, workspacestate.OrganizationGroup{ID: group.ID, Title: group.Title, Members: []string{}})
		}
	}
	importOrganizationMembers(o, nodes, legacy.Groups, canonicalByAlias)
	if initial {
		importOrganizationOrder(o, nodes, legacy.SessionOrder, legacy.Topics, canonicalByAlias, manual)
	}
	o.MigrationVersion = 1
}

func projectedShellOrganization(workspace workspacestate.Workspace, state workspacestate.State, sources []ProjectNode, legacy desktopProject) workspacestate.Organization {
	org := workspacestate.Organization{}
	if workspace.Organization != nil {
		org = workspace.Organization.Clone()
	}
	// Shells have no group filter. Without an old manual order there is
	// nothing to project, so ordinary refreshes do not rebuild import members.
	if org.MigrationVersion != 0 || !legacy.ManualSessionOrder && !legacy.ManualTopicOrder {
		return org
	}
	aliases := workspaceSourceAliases(state, workspace.ID)
	nodes := make([]ProjectNode, 0, len(workspace.SessionIDs)+len(sources))
	for _, id := range workspace.SessionIDs {
		ref := session.SessionRef{HostID: localDesktopHostID, SessionID: id}
		nodes = append(nodes, ProjectNode{Session: &ref, TopicID: state.Presentation[id].TopicID, IdentityAliases: aliases[id]})
	}
	nodes = append(nodes, sources...)
	projectLegacyOrganization(&org, nodes, legacy)
	return org
}

func workspaceSourceAliases(state workspacestate.State, workspaceID string) map[string][]string {
	result := map[string][]string{}
	for _, m := range state.SourceMappings {
		if m.WorkspaceID != workspaceID {
			continue
		}
		result[m.SessionID] = append(result[m.SessionID], "source\x00local\x00"+m.SourceKey)
		if sourceMappingHasPathAlias(m) {
			result[m.SessionID] = append(result[m.SessionID], "path\x00"+m.Path)
		}
	}
	for _, aliases := range result {
		slices.Sort(aliases)
	}
	return result
}

func sourceAliases(state workspacestate.State, workspaceID, sessionID string) []string {
	aliases := []string{}
	for _, m := range state.SourceMappings {
		if m.WorkspaceID != workspaceID || m.SessionID != sessionID {
			continue
		}
		aliases = append(aliases, "source\x00local\x00"+m.SourceKey)
		if sourceMappingHasPathAlias(m) {
			aliases = append(aliases, "path\x00"+m.Path)
		}
	}
	slices.Sort(aliases)
	return aliases
}

func (a *App) GetSessionOrganization(workspace SessionOrganizationWorkspace) (SessionOrganizationSnapshot, error) {
	if workspace.HostID != "" && workspace.HostID != localDesktopHostID {
		return a.remoteSessionOrganization(workspace, nil, nil)
	}
	_, o, err := a.ensureSessionOrganization(workspace.Scope, workspace.WorkspaceRoot)
	return organizationSnapshot(o, err == nil), err
}

func (a *App) UpdateSessionOrganization(workspace SessionOrganizationWorkspace, expectedRevision uint64, mutation SessionOrganizationMutation) (SessionOrganizationSnapshot, error) {
	if workspace.HostID != "" && workspace.HostID != localDesktopHostID {
		return a.remoteSessionOrganization(workspace, &expectedRevision, &mutation)
	}
	id, _, err := a.ensureSessionOrganization(workspace.Scope, workspace.WorkspaceRoot)
	if err != nil {
		return SessionOrganizationSnapshot{}, err
	}
	resolved := []SessionTarget{}
	resolve := func(selector *SessionSelector) (string, error) {
		if selector == nil {
			return "", newSessionOperationError("target_not_found", "Select a session.")
		}
		target, e := a.resolveSessionTarget(*selector)
		if e != nil {
			return "", e
		}
		targetWorkspaceID, e := a.resolveDesktopWorkspaceID(a.bootContext(), target.Scope, target.WorkspaceRoot)
		if e != nil {
			return "", e
		}
		if targetWorkspaceID != id {
			return "", newSessionOperationError("target_changed", "The session moved to another workspace.")
		}
		resolved = append(resolved, target)
		var ref *session.SessionRef
		if target.SessionRef.SessionID != "" {
			ref = &target.SessionRef
		}
		return projectNodeSessionKey(ProjectNode{Session: ref, SessionPath: target.SessionPath, Source: selector.Source}), nil
	}
	key, anchor := "", ""
	if mutation.Kind == "move" || mutation.Kind == "set-group" {
		key, err = resolve(mutation.Target)
		if err != nil {
			return SessionOrganizationSnapshot{}, err
		}
	}
	if mutation.Kind == "move" {
		anchor, err = resolve(mutation.Anchor)
		if err != nil {
			return SessionOrganizationSnapshot{}, err
		}
	}
	o, applied, err := a.workspaceRegistry().UpdateOrganizationWithState(a.bootContext(), id, &expectedRevision, func(state *workspacestate.State, o *workspacestate.Organization) error {
		for _, target := range resolved {
			if target.SessionRef.SessionID == "" {
				continue
			}
			current := state.SessionStates[target.SessionRef.SessionID]
			if current.Lifecycle != workspacestate.Active || current.Generation != target.LifecycleGeneration || !slices.Contains(state.Workspaces[id].SessionIDs, target.SessionRef.SessionID) {
				return workspacestate.ErrMutationConflict
			}
		}
		return applyOrganizationMutation(o, mutation, key, anchor)
	})
	if err == nil && applied {
		a.emitProjectTreeMetadataChanged()
	}
	return organizationSnapshot(o, applied), err
}

// replaceSessionOrganizationGroups retains old RPC signatures while moving their
// persistence into the same transaction as ordering and lifecycle mutations.
func (a *App) replaceSessionOrganizationGroups(ctx context.Context, scope, root string, revision *uint64, groups []desktopGroup) (ProjectGroupsSnapshot, error) {
	id, _, err := a.ensureSessionOrganization(scope, root)
	if err != nil {
		return ProjectGroupsSnapshot{}, err
	}
	if err = validateSessionGroups(groups); err != nil {
		return ProjectGroupsSnapshot{}, err
	}
	state, err := a.workspaceRegistry().LoadProjection(ctx)
	if err != nil {
		return ProjectGroupsSnapshot{}, err
	}
	legacy, err := a.unadoptedLegacyTopics(ProjectTopicPageRequest{Scope: scope, WorkspaceRoot: root, Limit: 200}, map[string]bool{}, map[string]bool{})
	if err != nil {
		return ProjectGroupsSnapshot{}, err
	}
	nodes := append([]ProjectNode{}, legacy.Items...)
	for _, sid := range state.Workspaces[id].SessionIDs {
		if state.SessionStates[sid].Lifecycle != workspacestate.Active {
			continue
		}
		ref := session.SessionRef{HostID: localDesktopHostID, SessionID: sid}
		nodes = append(nodes, ProjectNode{Session: &ref, TopicID: state.Presentation[sid].TopicID})
	}
	o, applied, err := a.workspaceRegistry().UpdateOrganization(ctx, id, revision, func(o *workspacestate.Organization) error {
		next := []workspacestate.OrganizationGroup{}
		for _, g := range groups {
			members := []string{}
			for _, node := range nodes {
				key := projectNodeSessionKey(node)
				if o.Imported[key] && desktopGroupContainsNode(g, node) && !slices.Contains(members, key) {
					members = append(members, key)
				}
			}
			for _, key := range g.SessionKeys {
				if !o.Imported[key] {
					return workspacestate.ErrMutationConflict
				}
				if !slices.Contains(members, key) {
					members = append(members, key)
				}
			}
			group := workspacestate.OrganizationGroup{ID: g.ID, Title: g.Title, Members: members}
			for _, existing := range o.Groups {
				if existing.ID == g.ID {
					group = existing
					group.Title, group.Members = g.Title, members
					break
				}
			}
			next = append(next, group)
		}
		o.Groups = next
		return nil
	})
	if err == nil && applied {
		a.emitProjectTreeMetadataChanged()
	}
	s := organizationSnapshot(o, applied)
	return ProjectGroupsSnapshot{Groups: s.Groups, Revision: s.Revision, Applied: applied}, err
}

func applyOrganizationMutation(o *workspacestate.Organization, mutation SessionOrganizationMutation, key, anchor string) error {
	switch mutation.Kind {
	case "move":
		if key == anchor {
			return nil
		}
		if !o.Imported[key] || !o.Imported[anchor] {
			return workspacestate.ErrMutationConflict
		}
		if mutation.Position != "before" && mutation.Position != "after" {
			return fmt.Errorf("invalid position")
		}
		o.Order = slices.DeleteFunc(o.Order, func(v string) bool { return v == key })
		i := slices.Index(o.Order, anchor)
		if i < 0 {
			return workspacestate.ErrMutationConflict
		}
		if mutation.Position == "after" {
			i++
		}
		o.Order = slices.Insert(o.Order, i, key)
		o.ManualOrderEnabled = true
	case "set-group":
		if !o.Imported[key] {
			return workspacestate.ErrMutationConflict
		}
		found := mutation.GroupID == ""
		for _, g := range o.Groups {
			found = found || g.ID == mutation.GroupID
		}
		if !found {
			return workspacestate.ErrMutationConflict
		}
		for i := range o.Groups {
			o.Groups[i].Members = slices.DeleteFunc(o.Groups[i].Members, func(v string) bool { return v == key })
			if o.Groups[i].ID == mutation.GroupID {
				o.Groups[i].Members = append(o.Groups[i].Members, key)
			}
		}
	case "create-group":
		if len(o.Groups) >= maxSessionGroups {
			return fmt.Errorf("group limit exceeded")
		}
		if err := validateSessionGroups([]desktopGroup{{ID: mutation.GroupID, Title: mutation.Title}}); err != nil {
			return err
		}
		if strings.TrimSpace(mutation.GroupID) == "" || strings.TrimSpace(mutation.Title) == "" {
			return fmt.Errorf("group id and title required")
		}
		for _, g := range o.Groups {
			if g.ID == mutation.GroupID {
				return workspacestate.ErrMutationConflict
			}
		}
		o.Groups = append(o.Groups, workspacestate.OrganizationGroup{ID: mutation.GroupID, Title: strings.TrimSpace(mutation.Title), Members: []string{}})
	case "rename-group", "delete-group":
		if mutation.Kind == "rename-group" {
			if err := validateSessionGroups([]desktopGroup{{ID: mutation.GroupID, Title: mutation.Title}}); err != nil {
				return err
			}
		}
		index := slices.IndexFunc(o.Groups, func(g workspacestate.OrganizationGroup) bool { return g.ID == mutation.GroupID })
		if index < 0 {
			return workspacestate.ErrMutationConflict
		}
		if mutation.Kind == "delete-group" {
			o.Groups = slices.Delete(o.Groups, index, index+1)
		} else {
			if strings.TrimSpace(mutation.Title) == "" {
				return fmt.Errorf("title required")
			}
			o.Groups[index].Title = strings.TrimSpace(mutation.Title)
		}
	default:
		return fmt.Errorf("unsupported organization mutation")
	}
	return nil
}

func importOrganizationMembers(o *workspacestate.Organization, nodes []ProjectNode, groups []desktopGroup, canonicalByAlias map[string]string) {
	for _, n := range nodes {
		if n.Session == nil && n.SessionPath == "" {
			continue
		}
		key := projectNodeSessionKey(n)
		if _, adopted := canonicalByAlias[key]; adopted {
			continue
		}
		if o.Imported[key] {
			continue
		}
		for _, old := range groups {
			included := desktopGroupContainsNode(old, n)
			for _, alias := range n.IdentityAliases {
				if slices.Contains(old.ExcludedSessionKeys, alias) {
					included = false
					break
				}
				if slices.Contains(old.SessionKeys, alias) {
					included = true
				}
			}
			if included {
				for i := range o.Groups {
					if o.Groups[i].ID == old.ID {
						o.Groups[i].Members = append(o.Groups[i].Members, key)
						break
					}
				}
			}
		}
		if !slices.Contains(o.Order, key) {
			o.Order = append(o.Order, key)
		}
		o.Imported[key] = true
	}
}

func importOrganizationOrder(o *workspacestate.Organization, nodes []ProjectNode, order, topicOrder []string, canonicalByAlias map[string]string, manual bool) {
	if manual && len(order) == 0 {
		for _, topic := range topicOrder {
			for _, node := range nodes {
				if node.TopicID == topic {
					order = append(order, projectNodeSessionKey(node))
				}
			}
		}
	}
	if len(order) > 0 {
		next := []string{}
		for _, key := range order {
			if canonical, ok := canonicalByAlias[key]; ok {
				key = canonical
			}
			if o.Imported[key] && !slices.Contains(next, key) {
				next = append(next, key)
			}
		}
		for _, key := range o.Order {
			if !slices.Contains(next, key) {
				next = append(next, key)
			}
		}
		o.Order = next
	}
}
