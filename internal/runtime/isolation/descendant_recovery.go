package isolation

import "context"

func (s *Store) PreviewResultRecovery(ctx context.Context, id string) (*RecoveryTarget, error) {
	store, err := s.managementStore(ctx, id)
	if err != nil {
		return nil, err
	}
	return store.previewLocalResultRecovery(ctx, id)
}

func (s *Store) previewLocalResultRecovery(ctx context.Context, id string) (*RecoveryTarget, error) {
	preview, err := s.PreviewRecovery(ctx)
	if err != nil || preview == nil {
		return nil, err
	}
	return &RecoveryTarget{ResultID: id, Workspace: s.workspace, Revision: preview.Revision, Paths: preview.Paths}, nil
}

func (s *Store) RecoverResultExpected(ctx context.Context, id, workspace, revision string, authorize SourceAuthorizer) error {
	if authorize == nil {
		return errUnavailable
	}
	store, err := s.managementStore(ctx, id)
	if err != nil {
		return err
	}
	if !sameDir(store.workspace, workspace) {
		return staleRecovery()
	}
	return store.RecoverExpected(ctx, revision, func(ctx context.Context, paths []string) error {
		if sameDir(store.workspace, s.workspace) {
			return authorize(ctx, paths)
		}
		mapped, err := MapSourcePaths(store.workspace, s.workspace, paths)
		if err != nil {
			return err
		}
		return authorize(ctx, mapped)
	})
}

func addDescendantRecoveries(ctx context.Context, routes map[string]managementRoute, out *Catalog) error {
	type observation struct {
		target *RecoveryTarget
		err    error
	}
	seen := map[string]observation{}
	for index := range out.Results {
		if err := ctx.Err(); err != nil {
			return err
		}
		row := &out.Results[index]
		if row.ParentID == "" || row.State != Applying {
			continue
		}
		id := row.ID
		if row.Kind == "integration" {
			id = row.ResultID
		}
		route, exists := routes[id]
		if !exists {
			continue
		}
		key := route.store.managedRoot + "\x00" + route.store.workspace
		observed, exists := seen[key]
		if !exists {
			observed.target, observed.err = route.store.previewLocalResultRecovery(ctx, id)
			seen[key] = observed
			if observed.err != nil {
				out.Diagnostics = append(out.Diagnostics, CatalogDiagnostic{Record: id, Reason: observed.err.Error()})
			}
		}
		if observed.err == nil && observed.target != nil {
			target := *observed.target
			target.ResultID = id
			row.Recovery = &target
			row.Actions = append(row.Actions, "recover")
		}
	}
	return nil
}
