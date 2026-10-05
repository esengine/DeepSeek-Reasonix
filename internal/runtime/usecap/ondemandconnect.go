package usecap

import (
	"context"
	"encoding/json"
	"fmt"

	"reasonix/internal/ext/plugin"
)

// onDemandMCPConnect is the deferred first-discovery target: it connects the
// server post-approval and returns the live tool directory.
type onDemandMCPConnect struct {
	proxy  *UseCapabilityTool
	spec   plugin.Spec
	server string
}

func (o *onDemandMCPConnect) Name() string { return plugin.MCPConnectPermissionName(o.server) }

func (o *onDemandMCPConnect) Description() string {
	return "connect MCP server " + o.server + " on demand and list its tools"
}

func (o *onDemandMCPConnect) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }

func (o *onDemandMCPConnect) ReadOnly() bool { return false }

// MCPLifecycleConnect marks this target as an MCP connect-and-list lifecycle
// action for Planner authorization (not a remote tools/call).
func (o *onDemandMCPConnect) MCPLifecycleConnect() bool { return true }

func (o *onDemandMCPConnect) MCPServerAuthorized() bool {
	return o.spec.ServerAuthorized()
}

func (o *onDemandMCPConnect) MCPServerName() string { return o.server }

func (o *onDemandMCPConnect) ReadOnlyExecutionHostMutation() bool { return true }

func (o *onDemandMCPConnect) ReadOnlyExecutionBlockReason() string {
	if !o.spec.ServerAuthorized() {
		return "start an unauthorized MCP server (install it or complete project identity approval first)"
	}
	return "connect this MCP server from a parent session first"
}

func (o *onDemandMCPConnect) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	// Zero process/network start when authorization, enable state, or exact
	// runtime identity changed after resolve.
	spec, unlock, err := o.proxy.lockAuthorizedRuntimeServer(ctx, o.server)
	if err != nil {
		msg := err.Error()
		if o.proxy.ledger != nil {
			o.proxy.ledger.MarkUnavailable("mcp-server:"+o.server, msg)
		}
		return "", err
	}
	defer unlock()
	if !plugin.MCPRuntimeSpecMatches(spec, o.spec) {
		return "", fmt.Errorf("MCP server %q runtime identity changed after resolution; retry so Reasonix can bind the current configuration", o.server)
	}
	if _, err := o.proxy.ensureServerToolsForSpec(ctx, o.server, spec); err != nil {
		if o.proxy.ledger != nil {
			o.proxy.ledger.MarkUnavailable("mcp-server:"+o.server, err.Error())
		}
		return "", err
	}
	return o.proxy.listServerToolsForSpec(ctx, o.server, spec)
}
