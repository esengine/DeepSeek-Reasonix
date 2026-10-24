package isolation

import (
	"context"
	"path/filepath"
	"slices"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/tool"
)

type managementRoute struct {
	store  *Store
	entry  durableEntry
	parent string
}

type managementGraph struct {
	root     *Store
	records  map[string]map[string]storedResult
	routes   map[string]managementRoute
	visiting map[string]bool
}

func (s *Store) managementRoutes(ctx context.Context) (map[string]managementRoute, error) {
	g := &managementGraph{root: s, records: map[string]map[string]storedResult{}, routes: map[string]managementRoute{}, visiting: map[string]bool{}}
	records, err := g.index(s)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if !sameDir(record.entry.Workspace, s.workspace) {
			continue
		}
		if err := g.visit(ctx, managementRoute{store: s, entry: record.entry}); err != nil {
			return nil, err
		}
	}
	return g.routes, nil
}

func (g *managementGraph) index(store *Store) (map[string]storedResult, error) {
	root, err := fileutil.CanonicalWritePath(store.managedRoot)
	if err != nil {
		return nil, err
	}
	if records, ok := g.records[root]; ok {
		return records, nil
	}
	all, err := store.resultRecords()
	if err != nil {
		return nil, err
	}
	records := map[string]storedResult{}
	for _, record := range all {
		previous, exists := records[record.entry.ID]
		if exists {
			if !terminalPromotionPair(previous, record) {
				return nil, inconsistentManagementRoute("duplicate isolation identity retained")
			}
			if !previous.entry.legacy {
				continue
			}
		}
		records[record.entry.ID] = record
	}
	g.records[root] = records
	return records, nil
}

func (g *managementGraph) visit(ctx context.Context, route managementRoute) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id := route.entry.ID
	if g.visiting[id] {
		return inconsistentManagementRoute("cyclic isolation dependency retained")
	}
	if prior, exists := g.routes[id]; exists {
		if !sameDir(prior.store.managedRoot, route.store.managedRoot) || prior.parent != route.parent {
			return inconsistentManagementRoute("ambiguous descendant identity retained")
		}
		return nil
	}
	if len(g.routes) >= 4096 {
		return inconsistentManagementRoute("isolation dependency traversal limit reached; results retained")
	}
	g.routes[id] = route
	parent := route.entry
	if parent.Lineage == nil || !supportedResultVersion(parent) || !supportedLifecycle(parent) {
		return nil
	}
	g.visiting[id] = true
	defer delete(g.visiting, id)
	ids := make([]string, 0, len(parent.Lineage.Children))
	for childID := range parent.Lineage.Children {
		ids = append(ids, childID)
	}
	slices.Sort(ids)
	for _, childID := range ids {
		child := parent.Lineage.Children[childID]
		if !validID(childID) || child.ID != childID || !filepath.IsAbs(child.Root) ||
			!sameDir(child.Workspace, parent.Cand.WorkspaceRoot) {
			return inconsistentManagementRoute("descendant registration does not target its direct parent")
		}
		store := NewStore(child.Root, child.Workspace, parent.Cand.Repo).WithMutationCoordinator(g.root.mutations)
		records, err := g.index(store)
		if err != nil {
			return err
		}
		record, present := records[childID]
		if !present {
			continue
		}
		d := record.entry
		if !sameDir(d.Workspace, child.Workspace) || d.Snap.Repo != parent.Cand.Repo.Top() ||
			!supportedResultVersion(d) || !supportedLifecycle(d) || d.Lineage == nil || !matchesParentResult(d.Lineage.Parent, parent.entry(), child.Token) {
			return inconsistentManagementRoute("descendant backlink or target identity changed; results retained")
		}
		if err := g.visit(ctx, managementRoute{store: store, entry: d, parent: id}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) managementStore(ctx context.Context, id string) (*Store, error) {
	if s == nil {
		return nil, errUnavailable
	}
	routes, err := s.managementRoutes(ctx)
	if err != nil {
		return nil, err
	}
	if route, ok := routes[id]; ok {
		return route.store, nil
	}
	return nil, tool.Refusal{Code: CodeUnknownID, Message: "isolated result is unavailable in this workspace's registered dependency graph"}
}

func inconsistentManagementRoute(message string) error {
	return tool.Refusal{Code: CodeUnsupportedState, Message: message}
}
