package main

import (
	"errors"
	"fmt"

	"reasonix/internal/config"
	"reasonix/internal/mcpdiag"
	"reasonix/internal/plugin"
)

func reconcileRemovedMCPAuthentication(name string, roots []string) error {
	seenRoots := map[string]bool{}
	var cleanupErrors []error
	for _, root := range roots {
		if seenRoots[root] {
			continue
		}
		seenRoots[root] = true
		remainingResource := ""
		entry, found, err := desktopEffectiveMCPServer(root, name)
		if err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("reload MCP configuration for %q: %w", name, err))
			continue
		}
		if found {
			remainingResource = mcpdiag.HTTPMCPOAuthResource(entry.Type, entry.URL, mcpdiag.HasAuthConfig(entry.Headers, entry.Env, entry.URL))
		}
		if _, err := plugin.ReconcileHTTPMCPOAuthAfterRemoval(plugin.Spec{
			Name: name, StateDir: plugin.MCPStateDir(config.ReasonixHomeDir(), root, name),
		}, remainingResource); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("reconcile OAuth state for %q: %w", name, err))
		}
	}
	return errors.Join(cleanupErrors...)
}

func (a *App) mcpWorkspaceRoots(activeRoot string) []string {
	roots := []string{activeRoot}
	a.mu.RLock()
	for _, tab := range a.runtimeTabsLocked() {
		if tab != nil && tab.WorkspaceRoot != "" {
			roots = append(roots, tab.WorkspaceRoot)
		}
	}
	a.mu.RUnlock()
	return roots
}
