package cli

import (
	"reasonix/internal/config"
	"reasonix/internal/mcpdiag"
	"reasonix/internal/plugin"
)

func reconcileRemovedMCPOAuth(workspace, name string) error {
	cfg, err := config.LoadForRoot(workspace)
	if err != nil {
		return err
	}
	remainingResource := ""
	for _, entry := range cfg.Plugins {
		if entry.Name != name {
			continue
		}
		remainingResource = mcpdiag.HTTPMCPOAuthResource(entry.Type, entry.URL, mcpdiag.HasAuthConfig(entry.Headers, entry.Env, entry.URL))
		break
	}
	_, err = plugin.ReconcileHTTPMCPOAuthAfterRemoval(plugin.Spec{
		Name: name, StateDir: plugin.MCPStateDir(config.ReasonixHomeDir(), workspace, name),
	}, remainingResource)
	return err
}
