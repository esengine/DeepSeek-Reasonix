package main

import (
	"context"
	"fmt"
	"os"
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
	// SortMode is the activity order the sidebar shows while no manual order exists.
	SortMode string `json:"sortMode,omitempty"`
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
	return a.ensureSessionOrganizationSources(scope, root, false)
}

func (a *App) ensureSessionOrganizationSources(scope, root string, mutation bool) (string, workspacestate.Organization, error) {
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
	if cacheable && !mutation {
		if organization, ok := a.desktopSessions.organizations.get(importKey); ok {
			return id, organization, nil
		}
	}
	legacy := legacyOrganizationPreferences(projects, scope, root)
	nodes := []ProjectNode{}
	// Registry identities are already loaded; retain them for transactional
	// fork/group attachment without consulting any session file or catalog page.
	aliases := workspaceSourceAliases(state, id)
	for _, sid := range workspace.SessionIDs {
		p := state.Presentation[sid]
		ref := session.SessionRef{HostID: localDesktopHostID, SessionID: sid}
		nodes = append(nodes, ProjectNode{Session: &ref, TopicID: p.TopicID, SessionPath: sessionRoute(sid), IdentityAliases: aliases[sid]})
	}
	// Default organization has no source-dependent preferences to import.
	// Do not enumerate every history page just to establish an empty group and
	// automatic ordering projection on the first sidebar request.
	if mutation || legacy.ManualSessionOrder || legacy.ManualTopicOrder || len(legacy.Groups) > 0 {
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
	// A topic aliases its initial placeholder only while it has one owner.
	// Build this once per workspace, not once per row of a large history page.
	owners := map[string]string{}
	for _, id := range state.Workspaces[workspaceID].SessionIDs {
		if topic := state.Presentation[id].TopicID; topic != "" {
			if _, seen := owners[topic]; seen {
				owners[topic] = ""
			} else {
				owners[topic] = id
			}
		}
	}
	for topic, id := range owners {
		if id != "" {
			result[id] = append(result[id], "topic\x00"+topic)
		}
	}
	for _, m := range state.SourceMappings {
		if m.WorkspaceID != workspaceID {
			continue
		}
		for _, key := range state.SourceKeys(m.SourceKey) {
			result[m.SessionID] = append(result[m.SessionID], "source\x00local\x00"+key)
		}
		if sourceMappingHasPathAlias(m) {
			// Lazy catalog pages use a path source key even for a single-head
			// DAG. Include both displayed identities in runtime merges and
			// lifecycle receipts, without claiming independent sibling heads.
			result[m.SessionID] = append(result[m.SessionID], "path\x00"+m.Path,
				"source\x00local\x00"+desktopSourceKey(m.Path, ""))
		}
	}
	aliasOwners := map[string]string{}
	for id, aliases := range result {
		for _, alias := range aliases {
			if owner, exists := aliasOwners[alias]; exists && owner != id {
				aliasOwners[alias] = ""
			} else if !exists {
				aliasOwners[alias] = id
			}
		}
	}
	for id, aliases := range result {
		// Old path spellings can normalize to the same source while retaining
		// independent durable receipts. Ambiguity is not an ownership alias.
		aliases = slices.DeleteFunc(aliases, func(alias string) bool { return aliasOwners[alias] != id })
		slices.Sort(aliases)
		result[id] = slices.Compact(aliases)
	}
	return result
}

func sourceAliases(state workspacestate.State, workspaceID, sessionID string) []string {
	return append([]string{}, workspaceSourceAliases(state, workspaceID)[sessionID]...)
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
	// Group edits address either a group or one explicit source. Only moving
	// in a manual order needs the complete relative order of other sources.
	// Old source-dependent preferences still take their normal import path.
	id, current, err := a.ensureSessionOrganizationSources(workspace.Scope, workspace.WorkspaceRoot, mutation.Kind == "move")
	if err != nil {
		return SessionOrganizationSnapshot{}, err
	}
	var shown []string
	if mutation.Kind == "move" && !current.ManualOrderEnabled {
		if shown, err = a.shownSessionOrder(workspace.Scope, workspace.WorkspaceRoot, mutation.SortMode); err != nil {
			return SessionOrganizationSnapshot{}, err
		}
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
		var ref *session.SessionRef
		if target.SessionRef.SessionID != "" {
			ref = &target.SessionRef
		} else {
			if _, err := os.Stat(target.SessionPath); err != nil {
				return "", newSessionOperationError("target_not_found", "The historical source is unavailable.")
			}
			// Both path-only compatibility selectors and explicit source refs
			// must name the same physical member used by the sidebar.
			head := ""
			if target.Source != nil {
				head = target.Source.HeadID
			} else if selector.Source != nil {
				head = selector.Source.HeadID
			}
			target.Source = &SessionSourceRef{HostID: localDesktopHostID, Path: target.SessionPath,
				HeadID: head, SourceKey: desktopSourceKey(target.SessionPath, head)}
		}
		resolved = append(resolved, target)
		return projectNodeSessionKey(ProjectNode{Session: ref, SessionPath: target.SessionPath, Source: target.Source}), nil
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
		if mutation.Kind == "move" && !o.ManualOrderEnabled {
			seedManualOrder(o, shown)
		}
		return applyResolvedOrganizationMutation(state, id, o, resolved, mutation, key, anchor)
	})
	if err == nil && applied {
		a.emitProjectTreeMetadataChanged()
	}
	return organizationSnapshot(o, applied), err
}

// shownSessionOrder lists a workspace in the activity order the sidebar is
// showing, so the first manual move starts from what the user was looking at.
func (a *App) shownSessionOrder(scope, root, sortMode string) ([]string, error) {
	keys := []string{}
	req := ProjectTopicPageRequest{Scope: scope, WorkspaceRoot: root, Limit: 200, SortMode: sortMode}
	for {
		page, err := a.unifiedProjectTopics(req)
		a.ReleaseReadSnapshot(page.SnapshotID)
		if err != nil {
			return nil, err
		}
		for _, node := range page.Items {
			for _, row := range expandSessionSourceRows(node) {
				keys = append(keys, projectNodeSessionKey(row))
			}
		}
		if page.NextCursor == "" {
			return keys, nil
		}
		if page.NextCursor == req.Cursor {
			return nil, fmt.Errorf("session order cursor did not advance")
		}
		req.Cursor = page.NextCursor
	}
}

// seedManualOrder replaces the attach order, which is oldest first and never
// what the sidebar showed, with the shown order. Unlisted keys keep their place
// after the listed ones.
func seedManualOrder(o *workspacestate.Organization, shown []string) {
	rank := make(map[string]int, len(shown))
	for i, key := range shown {
		if _, ok := rank[key]; !ok {
			rank[key] = i
		}
	}
	slices.SortStableFunc(o.Order, func(left, right string) int {
		l, lok := rank[left]
		r, rok := rank[right]
		switch {
		case lok && rok:
			return l - r
		case lok:
			return -1
		case rok:
			return 1
		}
		return 0
	})
}

// Run inside the registry transaction: resolution precedes the write lock, so
// source adoption and lifecycle changes must be checked again at commit time.
func applyResolvedOrganizationMutation(state *workspacestate.State, workspaceID string, o *workspacestate.Organization, resolved []SessionTarget, mutation SessionOrganizationMutation, key, anchor string) error {
	for _, target := range resolved {
		if target.SessionRef.SessionID == "" {
			if target.Source == nil || target.Source.SourceKey == "" {
				return workspacestate.ErrMutationConflict
			}
			if _, adopted, err := state.ResolveSource(target.Source.SourceKey); adopted || err != nil {
				return workspacestate.ErrMutationConflict
			}
			continue
		}
		current := state.SessionStates[target.SessionRef.SessionID]
		if current.Lifecycle != workspacestate.Active || current.Generation != target.LifecycleGeneration || !slices.Contains(state.Workspaces[workspaceID].SessionIDs, target.SessionRef.SessionID) {
			return workspacestate.ErrMutationConflict
		}
	}
	if mutation.Kind == "set-group" && !o.Imported[key] {
		// Imported also records explicit ungrouping so later preference
		// discovery cannot restore an older assignment.
		o.Imported[key] = true
		if !slices.Contains(o.Order, key) {
			o.Order = append(o.Order, key)
		}
	}
	return applyOrganizationMutation(o, mutation, key, anchor)
}

// replaceSessionOrganizationGroups retains old RPC signatures while moving their
// persistence into the same transaction as ordering and lifecycle mutations.
func (a *App) replaceSessionOrganizationGroups(ctx context.Context, scope, root string, revision *uint64, groups []desktopGroup) (ProjectGroupsSnapshot, error) {
	id, _, err := a.ensureSessionOrganizationSources(scope, root, true)
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
	case "reset-order":
		// Keep the recorded order and groups; time sorting ignores ranks while disabled.
		o.ManualOrderEnabled = false
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
	ordered := make(map[string]bool, len(o.Order)+len(nodes))
	for _, key := range o.Order {
		ordered[key] = true
	}
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
		if !ordered[key] {
			o.Order = append(o.Order, key)
			ordered[key] = true
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
