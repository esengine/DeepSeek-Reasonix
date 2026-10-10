package control

import (
	"errors"
	"fmt"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/mcpdiag"
	"reasonix/internal/ext/plugin"
)

type removedMCPState struct {
	fallback      config.PluginEntry
	fallbackFound bool
	cleanupErr    error
}

func reconcileRemovedMCPState(workspace, name string) removedMCPState {
	state := removedMCPState{}
	nextCfg, err := config.LoadForRoot(workspace)
	if err != nil {
		state.cleanupErr = fmt.Errorf("reload MCP configuration after removing %q: %w", name, err)
		return state
	}
	for _, candidate := range nextCfg.Plugins {
		if candidate.Name == name {
			state.fallback, state.fallbackFound = candidate, true
			break
		}
	}
	remainingResource := ""
	if state.fallbackFound {
		remainingResource = mcpdiag.HTTPMCPOAuthResource(state.fallback.Type, state.fallback.URL, mcpdiag.HasAuthConfig(state.fallback.Headers, state.fallback.Env, state.fallback.URL))
	}
	spec := plugin.Spec{Name: name, StateDir: plugin.MCPStateDir(config.ReasonixHomeDir(), workspace, name)}
	if _, err := plugin.ReconcileHTTPMCPOAuthAfterRemoval(spec, remainingResource); err != nil {
		state.cleanupErr = fmt.Errorf("reconcile OAuth state after removing MCP server %q: %w", name, err)
	}
	if !state.fallbackFound {
		if err := plugin.ForgetToolPins(spec.StateDir, name); err != nil {
			state.cleanupErr = errors.Join(state.cleanupErr, fmt.Errorf("forget approved tool definitions of MCP server %q: %w", name, err))
		}
	}
	return state
}
