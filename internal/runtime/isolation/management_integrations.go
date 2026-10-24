package isolation

import (
	"context"
	"path/filepath"
	"strings"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/tool"
)

type integrationManagementRoute struct {
	store   *Store
	attempt IntegrationAttempt
	parent  string
}

func managementIntegrations(ctx context.Context, routes map[string]managementRoute) (map[string]integrationManagementRoute, error) {
	seenRoots, seenIDs := map[string]bool{}, map[string]bool{}
	result := map[string]integrationManagementRoute{}
	for _, route := range routes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		root, err := fileutil.CanonicalWritePath(route.store.integrationRoot())
		if err != nil {
			return nil, err
		}
		if seenRoots[root] {
			continue
		}
		seenRoots[root] = true
		paths, err := route.store.integrationRecordPaths()
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			id := strings.TrimSuffix(filepath.Base(path), ".json")
			if seenIDs[id] {
				return nil, inconsistentManagementRoute("duplicate integration identity retained")
			}
			seenIDs[id] = true
			identity, err := readIntegrationIdentity(path, id)
			if err != nil {
				return nil, err
			}
			original, ok := routes[identity.ResultID]
			if !ok || !sameDir(original.store.integrationRoot(), route.store.integrationRoot()) {
				continue
			}
			attempt, _, err := original.store.readIntegration(id)
			if err != nil {
				return nil, err
			}
			if attempt.Owner != original.entry.ResumeOwner ||
				(!terminalState(attempt.State) && (attempt.ResultGeneration != original.entry.RunGeneration || attempt.ResultTree != original.entry.Tree)) {
				return nil, inconsistentManagementRoute("integration provenance differs from its registered result; records retained")
			}
			result[id] = integrationManagementRoute{store: original.store, attempt: attempt, parent: original.parent}
		}
	}
	return result, nil
}

func (s *Store) managementIntegrationStore(ctx context.Context, id string) (*Store, error) {
	if s == nil {
		return nil, errUnavailable
	}
	if _, err := s.integrationPath(id); err != nil {
		return nil, err
	}
	routes, err := s.managementRoutes(ctx)
	if err != nil {
		return nil, err
	}
	integrations, err := managementIntegrations(ctx, routes)
	if err != nil {
		return nil, err
	}
	if route, ok := integrations[id]; ok {
		return route.store, nil
	}
	return nil, tool.Refusal{Code: CodeUnknownID, Message: "integration attempt is unavailable in the registered dependency graph"}
}
