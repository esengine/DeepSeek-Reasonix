package isolation

import (
	"context"
	"slices"
	"strings"
)

func (s *Store) Catalog(ctx context.Context) (Catalog, error) {
	out, err := s.catalogLocal(ctx)
	if err != nil || s == nil {
		return out, err
	}
	routes, err := s.managementRoutes(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if len(out.Diagnostics) == 0 {
			out.Diagnostics = append(out.Diagnostics, CatalogDiagnostic{Record: "dependencies", Reason: err.Error()})
		}
		return out, nil
	}
	for _, route := range routes {
		if route.parent == "" {
			continue
		}
		row := route.store.resultSummary(route.entry)
		row.ParentID, row.TargetWorkspace = route.parent, route.entry.Workspace
		out.Results = append(out.Results, row)
	}
	integrations, err := managementIntegrations(ctx, routes)
	if err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		out.Diagnostics = append(out.Diagnostics, CatalogDiagnostic{Record: "dependency-integrations", Reason: err.Error()})
	}
	for _, route := range integrations {
		if route.parent == "" {
			continue
		}
		row := route.store.integrationSummary(route.attempt)
		row.ParentID, row.TargetWorkspace = route.parent, route.store.workspace
		out.Results = append(out.Results, row)
	}
	if err := addDescendantRecoveries(ctx, routes, &out); err != nil {
		return out, err
	}
	slices.SortFunc(out.Results, func(a, b ResultSummary) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}
