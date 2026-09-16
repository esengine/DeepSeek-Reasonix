package agent

import (
	"context"
	"fmt"
	"reasonix/internal/plugin"
)

// listServerTools renders the live tool directory of a connected server and
// refreshes the proxy snapshot on the way (via serverTools).
func (t *UseCapabilityTool) listServerTools(ctx context.Context, server string) (string, error) {
	tools, err := t.serverTools(ctx, server)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("connected MCP server %q; %d tools:\n%s", server, len(tools), inspectToolListJSON(server, tools)), nil
}

func (t *UseCapabilityTool) listServerToolsForSpec(ctx context.Context, server string, spec plugin.Spec) (string, error) {
	tools, err := t.serverToolsForSpec(ctx, server, spec)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("connected MCP server %q; %d tools:\n%s", server, len(tools), inspectToolListJSON(server, tools)), nil
}
